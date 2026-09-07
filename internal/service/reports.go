package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/runner"
	"gorm.io/gorm"
)

const maxReportOutputBytes int64 = 2 * 1024 * 1024

type capturedReport struct {
	ReportType   string              `json:"report_type"`
	Status       string              `json:"status"`
	Conclusion   string              `json:"conclusion"`
	Summary      string              `json:"summary"`
	Issues       []model.ReportIssue `json:"issues"`
	Markdown     string              `json:"markdown"`
	ErrorMessage string              `json:"error_message"`
}

func (s *Service) runReportCommandStage(ctx context.Context, project model.Project, task *model.DeployTask, name, command string) (stageOutcome, error) {
	stage := s.startStage(ctx, task, name)
	finish := func(status string, err error) {
		now := time.Now()
		stage.Status = status
		stage.FinishedAt = &now
		exit := 0
		if err != nil {
			exit = 1
			stage.ErrorMessage = err.Error()
		}
		stage.ExitCode = &exit
		_ = s.DB.WithContext(context.Background()).Save(&stage).Error
	}

	base, target := reportCommitRange(*task)
	report := model.Report{
		Type:           model.ReportTypeAIReview,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		CommitID:       target,
		BeforeCommitID: base,
		Status:         model.ReportFailed,
	}
	if task.TriggerType == model.TriggerRollback {
		report.Status = model.ReportSkipped
		report.Conclusion = "skipped"
		report.Summary = "AI review is skipped for rollback tasks."
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageSkipped, nil)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report skipped for rollback task")
		return stageOK, nil
	}
	if strings.TrimSpace(command) == "" {
		err := fmt.Errorf("report command is empty")
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		return stageOK, nil
	}
	if task.TriggerType == model.TriggerWebhook && (!validCommit(base) || !validCommit(target)) {
		err := fmt.Errorf("webhook report requires before and target commit ids")
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, err.Error())
		return stageOK, nil
	}
	capturingRunner, ok := s.Runner.(runner.CaptureCommandRunner)
	if !ok {
		err := fmt.Errorf("configured command runner does not support report capture")
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		return stageOK, nil
	}

	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report capture started")
	raw, err := capturingRunner.RunCapture(ctx, task.ID, name, command, reportCommandEnv(project, *task), maxReportOutputBytes)
	if err != nil {
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report capture failed: "+err.Error())
		return stageOK, nil
	}
	var captured capturedReport
	if err := json.Unmarshal(raw, &captured); err != nil {
		err = fmt.Errorf("report command returned invalid JSON: %w", err)
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report capture failed: invalid JSON")
		return stageOK, nil
	}
	if err = validateCapturedReport(captured); err != nil {
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		return stageOK, nil
	}
	report.Status = normalizeReportStatus(captured.Status)
	report.Conclusion = strings.TrimSpace(captured.Conclusion)
	report.Summary = strings.TrimSpace(captured.Summary)
	report.Issues = normalizeReportIssues(captured.Issues)
	report.Markdown = captured.Markdown
	report.ErrorMessage = strings.TrimSpace(captured.ErrorMessage)
	if report.Status == model.ReportFailed && report.ErrorMessage == "" {
		report.ErrorMessage = "AI review reported a failure"
	}
	if err := s.saveReport(context.Background(), &report); err != nil {
		finish(model.StageFailed, err)
		return stageOK, nil
	}
	if report.Status == model.ReportFailed {
		err := fmt.Errorf("%s", report.ErrorMessage)
		finish(model.StageFailed, err)
	} else if report.Status == model.ReportSkipped {
		finish(model.StageSkipped, nil)
	} else {
		finish(model.StageSuccess, nil)
	}
	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report captured")
	return stageOK, nil
}

func validateCapturedReport(report capturedReport) error {
	if report.ReportType != model.ReportTypeAIReview {
		return fmt.Errorf("report_type must be %q", model.ReportTypeAIReview)
	}
	switch strings.ToLower(strings.TrimSpace(report.Status)) {
	case model.ReportSuccess, model.ReportFailed, model.ReportSkipped:
	default:
		return fmt.Errorf("report status must be success, failed or skipped")
	}
	if strings.TrimSpace(report.Conclusion) == "" {
		return fmt.Errorf("report conclusion is required")
	}
	for i, issue := range report.Issues {
		if strings.TrimSpace(issue.Title) == "" {
			return fmt.Errorf("report issue %d title is required", i)
		}
		switch strings.ToLower(strings.TrimSpace(issue.Severity)) {
		case "high", "medium", "low":
		default:
			return fmt.Errorf("report issue %d severity is invalid", i)
		}
	}
	return nil
}

func reportCommitRange(task model.DeployTask) (string, string) {
	if task.TriggerType == model.TriggerWebhook {
		return strings.TrimSpace(task.BeforeCommitID), strings.TrimSpace(task.CommitID)
	}
	return "HEAD^", "HEAD"
}

func reportCommandEnv(project model.Project, task model.DeployTask) map[string]string {
	base, target := reportCommitRange(task)
	return map[string]string{
		"POSTDARE_TASK_ID":          strconv.FormatUint(task.ID, 10),
		"POSTDARE_TRIGGER_TYPE":     task.TriggerType,
		"POSTDARE_PROJECT_DIR":      project.AppDir,
		"POSTDARE_COMMIT_ID":        target,
		"POSTDARE_BEFORE_COMMIT_ID": base,
	}
}

func validCommit(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && strings.Trim(value, "0") != ""
}

func normalizeReportStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case model.ReportFailed:
		return model.ReportFailed
	case model.ReportSkipped:
		return model.ReportSkipped
	default:
		return model.ReportSuccess
	}
}

func normalizeReportIssues(issues []model.ReportIssue) []model.ReportIssue {
	if issues == nil {
		return []model.ReportIssue{}
	}
	for i := range issues {
		issues[i].Severity = strings.ToLower(strings.TrimSpace(issues[i].Severity))
		switch issues[i].Severity {
		case "high", "medium", "low":
		default:
			issues[i].Severity = "low"
		}
	}
	return issues
}

func (s *Service) saveReport(ctx context.Context, report *model.Report) error {
	var existing model.Report
	err := s.DB.WithContext(ctx).Where("task_id = ? AND type = ?", report.TaskID, report.Type).First(&existing).Error
	if err == nil {
		report.ID = existing.ID
		report.CreatedAt = existing.CreatedAt
		report.ShareEnabled = existing.ShareEnabled
		report.ShareTokenHash = existing.ShareTokenHash
		return s.DB.WithContext(ctx).Save(report).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.DB.WithContext(ctx).Create(report).Error
}
