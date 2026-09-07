package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/notifier"
	"github.com/hellodeveye/postdare-go/internal/runner"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrProjectBusy          = errors.New("project has a running deploy task")
	ErrProjectHasActiveTask = errors.New("project has pending or running deploy task")
	ErrMissingRollback      = errors.New("project rollback_cmd is empty")
	ErrTaskNotFound         = errors.New("deploy task not found")
	ErrTaskNotCancelable    = errors.New("deploy task is not pending or running")
	ErrServiceShuttingDown  = errors.New("service is shutting down")
)

type Service struct {
	DB       *gorm.DB
	Config   *config.Config
	Runner   runner.CommandRunner
	Hub      *sse.Hub
	Notifier *notifier.Notifier
	Logger   *zap.Logger

	cancelMu     sync.Mutex
	cancels      map[uint64]context.CancelFunc
	shuttingDown bool
	taskWG       sync.WaitGroup
}

func New(database *gorm.DB, cfg *config.Config, hub *sse.Hub, logger *zap.Logger) *Service {
	return &Service{
		DB:     database,
		Config: cfg,
		Runner: &runner.LocalCommandRunner{
			LogDir:  cfg.Deploy.LogDir,
			Timeout: cfg.CommandTimeout(),
			Hub:     hub,
			Logger:  logger,
		},
		Hub:      hub,
		Notifier: notifier.New(logger),
		Logger:   logger,
		cancels:  map[uint64]context.CancelFunc{},
	}
}

func (s *Service) DeleteProject(ctx context.Context, projectID uint64) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, projectID).Error; err != nil {
			return err
		}

		var activeCount int64
		if err := tx.Model(&model.DeployTask{}).
			Where("project_id = ? AND status IN ?", projectID, []string{model.TaskPending, model.TaskRunning}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return ErrProjectHasActiveTask
		}

		var taskIDs []uint64
		if err := tx.Model(&model.DeployTask{}).Where("project_id = ?", projectID).Pluck("id", &taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) > 0 {
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.Report{}).Error; err != nil {
				return err
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.DeployTaskStage{}).Error; err != nil {
				return err
			}
			if err := tx.Where("project_id = ?", projectID).Delete(&model.DeployTask{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? OR project_key = ?", projectID, project.ProjectKey).Delete(&model.WebhookEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&project).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *Service) CancelTask(ctx context.Context, taskID uint64) error {
	var task model.DeployTask
	if err := s.DB.WithContext(ctx).Select("id", "status").First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTaskNotFound
		}
		return err
	}
	if task.Status != model.TaskPending && task.Status != model.TaskRunning {
		return ErrTaskNotCancelable
	}
	s.cancelTask(taskID)
	result := s.DB.WithContext(ctx).Model(&model.DeployTask{}).
		Where("id = ? AND status IN ?", taskID, []string{model.TaskPending, model.TaskRunning}).
		Updates(map[string]interface{}{
			"status":      model.TaskCanceled,
			"fail_reason": "canceled by user",
			"finished_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrTaskNotCancelable
	}
	return nil
}

func (s *Service) ReconcileInterruptedTasks() error {
	now := time.Now()
	return s.DB.Model(&model.DeployTask{}).
		Where("status IN ?", []string{model.TaskPending, model.TaskRunning}).
		Updates(map[string]interface{}{
			"status":      model.TaskFailed,
			"fail_reason": "task interrupted by server restart",
			"finished_at": now,
		}).Error
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.cancelMu.Lock()
	s.shuttingDown = true
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.cancelMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		s.taskWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) unregisterCancel(taskID uint64) {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	delete(s.cancels, taskID)
}

func (s *Service) startTask(taskID uint64) error {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	if s.shuttingDown {
		s.cancelMu.Unlock()
		cancel()
		return ErrServiceShuttingDown
	}
	s.cancels[taskID] = cancel
	s.taskWG.Add(1)
	s.cancelMu.Unlock()

	go func() {
		defer s.taskWG.Done()
		defer s.unregisterCancel(taskID)
		defer cancel()
		s.ExecuteTask(ctx, taskID)
	}()
	return nil
}

func (s *Service) cancelTask(taskID uint64) {
	s.cancelMu.Lock()
	cancel := s.cancels[taskID]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Service) isShuttingDown() bool {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	return s.shuttingDown
}

func (s *Service) isCanceled(ctx context.Context, taskID uint64) bool {
	var task model.DeployTask
	if err := s.DB.WithContext(ctx).Select("status").First(&task, taskID).Error; err != nil {
		return false
	}
	return task.Status == model.TaskCanceled
}
