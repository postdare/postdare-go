package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/runner"
	"github.com/hellodeveye/postdare-go/internal/webhook"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) CreateDeployTask(ctx context.Context, project model.Project, triggerType string, ev *webhook.Event) (*model.DeployTask, error) {
	if s.isShuttingDown() {
		return nil, ErrServiceShuttingDown
	}
	if triggerType == model.TriggerRollback && strings.TrimSpace(project.RollbackCmd) == "" {
		return nil, ErrMissingRollback
	}
	var task *model.DeployTask
	if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lockedProject model.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedProject, project.ID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.DeployTask{}).
			Where("project_id = ? AND status IN ?", project.ID, []string{model.TaskPending, model.TaskRunning}).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrProjectBusy
		}
		task = &model.DeployTask{
			ProjectID:   project.ID,
			TriggerType: triggerType,
			GitProvider: project.GitProvider,
			Branch:      project.Branch,
			Status:      model.TaskPending,
		}
		if ev != nil {
			task.GitProvider = string(ev.Provider)
			task.Branch = ev.Branch
			task.CommitID = ev.CommitID
			task.BeforeCommitID = ev.BeforeCommitID
			task.CommitMessage = ev.CommitMessage
			task.CommitAuthor = ev.CommitAuthor
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		task.LogFile = filepath.Join(s.Config.Deploy.LogDir, fmt.Sprintf("%d.log", task.ID))
		if err := tx.Model(task).Update("log_file", task.LogFile).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.startTask(task.ID); err != nil {
		now := time.Now()
		_ = s.DB.WithContext(ctx).Model(task).Updates(map[string]interface{}{
			"status":      model.TaskFailed,
			"fail_reason": err.Error(),
			"finished_at": now,
		}).Error
		return nil, err
	}
	return task, nil
}

func (s *Service) ExecuteTask(ctx context.Context, taskID uint64) {
	var task model.DeployTask
	if err := s.DB.WithContext(ctx).First(&task, taskID).Error; err != nil {
		s.Logger.Error("load task failed", zap.Uint64("task_id", taskID), zap.Error(err))
		return
	}
	if task.Status == model.TaskCanceled {
		return
	}
	var project model.Project
	if err := s.DB.WithContext(ctx).First(&project, task.ProjectID).Error; err != nil {
		s.failTask(ctx, &task, "load_project", err)
		return
	}
	started := time.Now()
	task.StartedAt = &started
	task.Status = model.TaskRunning
	task.LogFile = filepath.Join(s.Config.Deploy.LogDir, fmt.Sprintf("%d.log", task.ID))
	_ = os.MkdirAll(s.Config.Deploy.LogDir, 0o755)
	_ = s.DB.WithContext(ctx).Save(&task).Error
	runner.AppendLog(task.LogFile, s.Hub, task.ID, "task", "task started")

	if task.TriggerType == model.TriggerRollback {
		s.executeRollback(ctx, project, &task)
		return
	}
	s.executeDeploy(ctx, project, &task)
}

func (s *Service) executeDeploy(ctx context.Context, project model.Project, task *model.DeployTask) {
	for _, stage := range DeployStages(project) {
		if !shouldRunInMainFlow(stage) {
			continue
		}
		outcome, err := s.runProjectStage(ctx, project, task, stage)
		switch outcome {
		case stageOK:
			continue
		case stageCanceled:
			s.finishTask(context.Background(), task, model.TaskCanceled, "canceled by user")
			return
		default: // stageFailed
			if stage.ContinueOnError {
				runner.AppendLog(task.LogFile, s.Hub, task.ID, stage.Name, fmt.Sprintf("stage failed but continue_on_error is set, continuing: %v", err))
				continue
			}
			s.failTask(ctx, task, stage.Name, err)
			s.executeDeferredStages(context.Background(), project, task)
			return
		}
	}
	s.finishTask(ctx, task, model.TaskSuccess, "")
	s.executeDeferredStages(context.Background(), project, task)
}

func (s *Service) executeRollback(ctx context.Context, project model.Project, task *model.DeployTask) {
	if !s.executeCommandStage(ctx, project, task, "rollback", project.RollbackCmd) {
		if task.Status != model.TaskCanceled {
			s.executeDeferredStages(context.Background(), project, task)
		}
		return
	}
	s.finishTask(ctx, task, model.TaskRollbacked, "")
	s.executeDeferredStages(context.Background(), project, task)
}

func (s *Service) failTask(ctx context.Context, task *model.DeployTask, stage string, err error) {
	s.finishTask(ctx, task, model.TaskFailed, err.Error())
	task.CurrentStage = stage
	runner.AppendLog(task.LogFile, s.Hub, task.ID, stage, "task failed: "+err.Error())
}

func (s *Service) finishTask(ctx context.Context, task *model.DeployTask, status string, failReason string) {
	now := time.Now()
	task.Status = status
	task.FailReason = failReason
	task.FinishedAt = &now
	_ = s.DB.WithContext(ctx).Model(task).Updates(map[string]interface{}{
		"status":        status,
		"fail_reason":   failReason,
		"finished_at":   now,
		"current_stage": task.CurrentStage,
	}).Error
	runner.AppendLog(task.LogFile, s.Hub, task.ID, "task", "task finished with status "+status)
}
