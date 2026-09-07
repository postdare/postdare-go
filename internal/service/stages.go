package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/runner"
)

// stageOutcome is the result of running a single command stage without any
// task-level side effects. Callers decide how a failure affects the task
// (deploy honors continue_on_error, rollback always aborts).
type stageOutcome int

const (
	stageOK stageOutcome = iota
	stageFailed
	stageCanceled
)

// runCommandStage executes one shell-command stage and records its DeployTaskStage row.
// It never mutates task-level status; on failure it returns the underlying error
// so the caller can surface it via failTask when appropriate.
func (s *Service) runCommandStage(ctx context.Context, project model.Project, task *model.DeployTask, name string, command string) (stageOutcome, error) {
	if s.isCanceled(ctx, task.ID) {
		return stageCanceled, nil
	}
	stage := s.startStage(ctx, task, name)
	if strings.TrimSpace(command) == "" {
		now := time.Now()
		stage.Status = model.StageSkipped
		stage.FinishedAt = &now
		_ = s.DB.WithContext(ctx).Save(&stage).Error
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "stage skipped: empty command")
		return stageOK, nil
	}
	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "stage started")
	err := s.Runner.RunWithEnv(ctx, task.ID, name, command, stageCommandEnv(project, *task))
	now := time.Now()
	stage.FinishedAt = &now
	if err != nil {
		exit := 1
		stage.ExitCode = &exit
		stage.Status = model.StageFailed
		stage.ErrorMessage = err.Error()
		_ = s.DB.WithContext(ctx).Save(&stage).Error
		if s.isCanceled(context.Background(), task.ID) {
			return stageCanceled, err
		}
		return stageFailed, err
	}
	exit := 0
	stage.ExitCode = &exit
	stage.Status = model.StageSuccess
	_ = s.DB.WithContext(ctx).Save(&stage).Error
	return stageOK, nil
}

// executeCommandStage runs a stage and applies the default "fail the whole task on
// error" behavior. Used by the rollback path, which has no per-stage error policy.
func (s *Service) executeCommandStage(ctx context.Context, project model.Project, task *model.DeployTask, name string, command string) bool {
	switch outcome, err := s.runCommandStage(ctx, project, task, name, command); outcome {
	case stageOK:
		return true
	case stageCanceled:
		s.finishTask(context.Background(), task, model.TaskCanceled, "canceled by user")
		return false
	default: // stageFailed
		s.failTask(ctx, task, name, err)
		return false
	}
}

// DeployStages returns the ordered stages configured for a deploy.
func DeployStages(project model.Project) []model.ProjectStage {
	return project.Stages
}

func shouldRunInMainFlow(stage model.ProjectStage) bool {
	return stage.Enabled && strings.TrimSpace(stage.RunWhen) == ""
}

func shouldRunDeferred(stage model.ProjectStage, terminalStatus string) bool {
	if !stage.Enabled {
		return false
	}
	switch stage.RunWhen {
	case model.ProjectStageRunWhenAlways:
		return true
	case model.ProjectStageRunWhenSuccess:
		return terminalStatus == model.TaskSuccess || terminalStatus == model.TaskRollbacked
	case model.ProjectStageRunWhenFailed:
		return terminalStatus == model.TaskFailed
	default:
		return false
	}
}

func (s *Service) executeDeferredStages(ctx context.Context, project model.Project, task *model.DeployTask) {
	for _, stage := range DeployStages(project) {
		if !shouldRunDeferred(stage, task.Status) {
			continue
		}
		outcome, err := s.runProjectStage(ctx, project, task, stage)
		if outcome == stageCanceled {
			return
		}
		if err != nil {
			runner.AppendLog(task.LogFile, s.Hub, task.ID, stage.Name, fmt.Sprintf("deferred stage failed: %v", err))
		}
	}
}

func (s *Service) runProjectStage(ctx context.Context, project model.Project, task *model.DeployTask, stage model.ProjectStage) (stageOutcome, error) {
	switch stage.Type {
	case model.ProjectStageTypeCommand:
		cfg, err := commandStageConfig(stage)
		if err != nil {
			return stageFailed, err
		}
		if reportType := model.NormalizeReportType(cfg.CaptureAs); reportType != "" {
			return s.runReportCommandStage(ctx, project, task, stage.Name, cfg.Command, reportType)
		}
		return s.runCommandStage(ctx, project, task, stage.Name, cfg.Command)
	case model.ProjectStageTypeHealthCheck:
		cfg, err := healthCheckStageConfig(stage)
		if err != nil {
			return stageFailed, err
		}
		return s.runHealthCheckStage(ctx, task, stage.Name, strings.TrimSpace(cfg.URL))
	case model.ProjectStageTypeOutboundWebhook:
		cfg, err := outboundWebhookStageConfig(stage)
		if err != nil {
			return stageFailed, err
		}
		return s.runOutboundWebhookStage(ctx, project, task, stage.Name, cfg)
	default:
		return stageFailed, fmt.Errorf("unsupported stage type %q", stage.Type)
	}
}

func commandStageConfig(stage model.ProjectStage) (model.CommandStageConfig, error) {
	var cfg model.CommandStageConfig
	if err := decodeStageConfig(stage, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func healthCheckStageConfig(stage model.ProjectStage) (model.HealthCheckStageConfig, error) {
	var cfg model.HealthCheckStageConfig
	if err := decodeStageConfig(stage, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func outboundWebhookStageConfig(stage model.ProjectStage) (model.OutboundWebhookStageConfig, error) {
	var cfg model.OutboundWebhookStageConfig
	if err := decodeStageConfig(stage, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func decodeStageConfig(stage model.ProjectStage, out interface{}) error {
	if len(stage.Config) == 0 {
		return nil
	}
	if err := json.Unmarshal(stage.Config, out); err != nil {
		return fmt.Errorf("invalid config for stage %s: %w", stage.Name, err)
	}
	return nil
}

func (s *Service) startStage(ctx context.Context, task *model.DeployTask, name string) model.DeployTaskStage {
	now := time.Now()
	task.CurrentStage = name
	_ = s.DB.WithContext(ctx).Model(task).Updates(map[string]interface{}{
		"current_stage": name,
	}).Error
	stage := model.DeployTaskStage{
		TaskID:    task.ID,
		Name:      name,
		Status:    model.StageRunning,
		StartedAt: &now,
	}
	_ = s.DB.WithContext(ctx).Create(&stage).Error
	return stage
}
