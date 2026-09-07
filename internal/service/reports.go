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

const (
	// A diff excerpt exists to make one finding checkable, not to carry the diff.
	// Neither limit ever rejects a review or a finding: an excerpt past the line
	// cap is cut, and one the report budget cannot hold is replaced by a note
	// saying so, because an excerpt that vanishes silently reads as a finding that
	// never had one.
	maxIssueDiffHunkLines  = 40
	maxReportDiffHunkBytes = 64 * 1024

	diffHunkTruncatedMarker = "... truncated"
	diffHunkOmittedMarker   = "... excerpt omitted: report excerpt budget reached"
)

type capturedReport struct {
	ReportType   string              `json:"report_type"`
	Status       string              `json:"status"`
	Conclusion   string              `json:"conclusion"`
	Summary      string              `json:"summary"`
	Issues       []model.ReportIssue `json:"issues"`
	Markdown     string              `json:"markdown"`
	ErrorMessage string              `json:"error_message"`
}

// runReportCommandStage runs a command stage whose stdout is a structured report
// and records the result as a model.Report. reportType comes from the stage's
// capture_as value and is both the stored type and the contract the script's
// report_type field must match.
//
// It always reports stageOK: a report is an artifact of the deploy, not a gate on
// it, so a failed capture is recorded on the Report row and left there.
func (s *Service) runReportCommandStage(ctx context.Context, project model.Project, task *model.DeployTask, name, command, reportType string) (stageOutcome, error) {
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
		Type:           reportType,
		ProjectID:      project.ID,
		TaskID:         task.ID,
		CommitID:       target,
		BeforeCommitID: base,
		Status:         model.ReportFailed,
		// A failed or skipped capture never reaches normalizeReportIssues, so seed
		// the list here: a nil slice persists as JSON null and clients that expect
		// an array break on it.
		Issues: []model.ReportIssue{},
	}
	// fail records why the capture produced no report, on the Report row and in
	// the deploy log, then closes the stage.
	fail := func(err error) (stageOutcome, error) {
		report.ErrorMessage = err.Error()
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageFailed, err)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report capture failed: "+err.Error())
		return stageOK, nil
	}

	if task.TriggerType == model.TriggerRollback {
		report.Status = model.ReportSkipped
		report.Conclusion = "skipped"
		report.Summary = "Report is skipped for rollback tasks."
		_ = s.saveReport(context.Background(), &report)
		finish(model.StageSkipped, nil)
		runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report skipped for rollback task")
		return stageOK, nil
	}
	if strings.TrimSpace(command) == "" {
		return fail(fmt.Errorf("report command is empty"))
	}
	if task.TriggerType == model.TriggerWebhook && (!validCommit(base) || !validCommit(target)) {
		return fail(fmt.Errorf("webhook report requires before and target commit ids"))
	}

	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report capture started")
	raw, err := s.Runner.RunCapture(ctx, task.ID, name, command, stageCommandEnv(project, *task), maxReportOutputBytes)
	if err != nil {
		return fail(err)
	}
	var captured capturedReport
	if err := json.Unmarshal(raw, &captured); err != nil {
		return fail(fmt.Errorf("report command returned invalid JSON: %w; output began %s", err, capturePreview(raw)))
	}
	if err := validateCapturedReport(captured, reportType); err != nil {
		return fail(err)
	}

	report.Status = normalizeReportStatus(captured.Status)
	report.Conclusion = strings.TrimSpace(captured.Conclusion)
	report.Summary = strings.TrimSpace(captured.Summary)
	report.Issues = normalizeReportIssues(captured.Issues)
	report.Markdown = captured.Markdown
	report.ErrorMessage = strings.TrimSpace(captured.ErrorMessage)
	if report.Status == model.ReportFailed && report.ErrorMessage == "" {
		report.ErrorMessage = "the report command reported a failure"
	}
	if err := s.saveReport(context.Background(), &report); err != nil {
		finish(model.StageFailed, err)
		return stageOK, nil
	}
	switch report.Status {
	case model.ReportFailed:
		finish(model.StageFailed, fmt.Errorf("%s", report.ErrorMessage))
	case model.ReportSkipped:
		finish(model.StageSkipped, nil)
	default:
		finish(model.StageSuccess, nil)
	}
	runner.AppendLog(task.LogFile, s.Hub, task.ID, name, "report captured")
	return stageOK, nil
}

// capturePreview quotes the head of a capture's stdout for an error message.
// Without it an operator sees only how the parser objected and not what the
// script actually printed, which is the part that says where to look.
func capturePreview(raw []byte) string {
	const maxPreviewRunes = 200
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "(no output)"
	}
	runes := []rune(text)
	if len(runes) > maxPreviewRunes {
		return strconv.Quote(string(runes[:maxPreviewRunes]) + "…")
	}
	return strconv.Quote(text)
}

func validateCapturedReport(report capturedReport, reportType string) error {
	if report.ReportType != reportType {
		return fmt.Errorf("report_type must be %q", reportType)
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
		if !model.IsSeverity(issue.Severity) {
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

// stageCommandEnv is injected into every command stage so scripts can locate the
// project checkout and the commit range the task is deploying.
func stageCommandEnv(project model.Project, task model.DeployTask) map[string]string {
	base, target := reportCommitRange(task)
	return map[string]string{
		"POSTDARE_TASK_ID":          strconv.FormatUint(task.ID, 10),
		"POSTDARE_TRIGGER_TYPE":     task.TriggerType,
		"POSTDARE_PROJECT_DIR":      project.AppDir,
		"POSTDARE_COMMIT_ID":        target,
		"POSTDARE_BEFORE_COMMIT_ID": base,
	}
}

// validCommit rejects both an empty commit id and the all-zero sha that Git
// hosts send as the "before" commit when a branch is first created.
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
	budget := maxReportDiffHunkBytes
	for i := range issues {
		issues[i].Severity = model.NormalizeSeverity(issues[i].Severity)
		issues[i].DiffHunk = trimDiffHunk(issues[i].DiffHunk, &budget)
	}
	return issues
}

// trimDiffHunk caps one excerpt at maxIssueDiffHunkLines and draws what remains
// from the report-wide budget. Both cuts are marked: a reader must be able to
// tell a shortened excerpt, and an omitted one, from a finding that simply came
// without evidence. Only an issue that carried no excerpt gets an empty string.
func trimDiffHunk(hunk string, budget *int) string {
	if strings.TrimSpace(hunk) == "" {
		return ""
	}
	lines := strings.Split(hunk, "\n")
	truncated := false
	if len(lines) > maxIssueDiffHunkLines {
		lines = lines[:maxIssueDiffHunkLines]
		truncated = true
	}
	trimmed := strings.Join(lines, "\n")
	if len(trimmed) > *budget {
		return diffHunkOmittedMarker
	}
	*budget -= len(trimmed)
	if truncated {
		trimmed += "\n" + diffHunkTruncatedMarker
	}
	return trimmed
}

func (s *Service) saveReport(ctx context.Context, report *model.Report) error {
	var existing model.Report
	err := s.DB.WithContext(ctx).Where("task_id = ? AND type = ?", report.TaskID, report.Type).First(&existing).Error
	if err == nil {
		report.ID = existing.ID
		report.CreatedAt = existing.CreatedAt
		report.ShareEnabled = existing.ShareEnabled
		report.ShareSalt = existing.ShareSalt
		report.ShareTokenHash = existing.ShareTokenHash
		return s.DB.WithContext(ctx).Save(report).Error
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.DB.WithContext(ctx).Create(report).Error
}
