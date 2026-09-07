package service

import (
	"context"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/runner"
)

func (s *Service) runOutboundWebhookStage(ctx context.Context, project model.Project, task *model.DeployTask, name string, cfg model.OutboundWebhookStageConfig) (stageOutcome, error) {
	if s.isCanceled(ctx, task.ID) {
		return stageCanceled, nil
	}
	stage := s.startStage(ctx, task, name)
	webhookURL := strings.TrimSpace(cfg.URL)
	if webhookURL == "" {
		now := time.Now()
		stage.Status = model.StageSkipped
		stage.FinishedAt = &now
		_ = s.DB.WithContext(ctx).Save(&stage).Error
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "stage skipped: outbound webhook url is empty")
		return stageOK, nil
	}
	cfg.URL = webhookURL
	var report *model.Report
	reportURL := ""
	if cfg.Template == "feishu_report_card" {
		var found model.Report
		if queryErr := s.DB.WithContext(ctx).Where("task_id = ? AND type = ?", task.ID, model.ReportTypeAIReview).First(&found).Error; queryErr == nil {
			shared, _, url, shareErr := s.RotateReportShare(ctx, found.ID)
			if shareErr == nil {
				report = &shared
				reportURL = url
			} else {
				report = &found
			}
		}
	}
	err := s.Notifier.SendOutboundWebhookWithReport(project, *task, cfg, report, reportURL)
	now := time.Now()
	stage.FinishedAt = &now
	if err != nil {
		exit := 1
		stage.ExitCode = &exit
		stage.Status = model.StageFailed
		stage.ErrorMessage = err.Error()
		_ = s.DB.WithContext(ctx).Save(&stage).Error
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "outbound webhook failed: "+err.Error())
		return stageFailed, err
	}
	exit := 0
	stage.ExitCode = &exit
	stage.Status = model.StageSuccess
	_ = s.DB.WithContext(ctx).Save(&stage).Error
	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "outbound webhook sent")
	return stageOK, nil
}
