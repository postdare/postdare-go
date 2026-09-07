package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hellodeveye/postdare-go/internal/model"
)

func TestReportCapturePersistsStructuredReport(t *testing.T) {
	svc := newTestService(t)
	project := model.Project{Name: "app", ProjectKey: "app", AppDir: t.TempDir(), GitProvider: model.GitProviderGitHub, Branch: "main"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerManual, Status: model.TaskSuccess, LogFile: svc.Config.Deploy.LogDir + "/1.log"}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	payload := `{"report_type":"ai_review","status":"success","conclusion":"issues_found","summary":"one issue","issues":[{"severity":"high","title":"race","location":"main.go:10"}],"markdown":"# Review"}`
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "printf '%s' '"+payload+"'", model.ReportTypeAIReview)
	if err != nil || outcome != stageOK {
		t.Fatalf("expected non-blocking success, got outcome=%v err=%v", outcome, err)
	}
	var report model.Report
	if err := svc.DB.Where("task_id = ?", task.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.Status != model.ReportSuccess || report.Conclusion != "issues_found" || len(report.Issues) != 1 || report.CommitID != "HEAD" {
		t.Fatalf("unexpected report: %+v", report)
	}
	logBytes, err := os.ReadFile(task.LogFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), "# Review") {
		t.Fatal("captured report stdout leaked into deploy log")
	}
}

func TestInvalidReportDoesNotFailDeployOutcome(t *testing.T) {
	svc := newTestService(t)
	project := model.Project{Name: "app", ProjectKey: "invalid", AppDir: t.TempDir(), GitProvider: model.GitProviderGitHub, Branch: "main"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerManual, Status: model.TaskSuccess, LogFile: svc.Config.Deploy.LogDir + "/2.log"}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "printf not-json", model.ReportTypeAIReview)
	if err != nil || outcome != stageOK {
		t.Fatalf("report errors must not alter deploy outcome: %v %v", outcome, err)
	}
	var report model.Report
	if err := svc.DB.Where("task_id = ?", task.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.Status != model.ReportFailed || !strings.Contains(report.ErrorMessage, "invalid JSON") {
		t.Fatalf("unexpected failed report: %+v", report)
	}
}

func TestRollbackReportIsSkipped(t *testing.T) {
	svc := newTestService(t)
	project := model.Project{Name: "app", ProjectKey: "rollback-report", AppDir: t.TempDir(), GitProvider: model.GitProviderGitHub, Branch: "main"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerRollback, Status: model.TaskRollbacked, LogFile: svc.Config.Deploy.LogDir + "/3.log"}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	_, _ = svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "exit 99", model.ReportTypeAIReview)
	var report model.Report
	if err := svc.DB.Where("task_id = ?", task.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.Status != model.ReportSkipped {
		t.Fatalf("expected skipped, got %s", report.Status)
	}
}

func TestWebhookReportWithoutCommitRangeIsRecordedFailed(t *testing.T) {
	svc := newTestService(t)
	project := model.Project{Name: "app", ProjectKey: "missing-commit", AppDir: t.TempDir(), GitProvider: model.GitProviderGitHub, Branch: "main"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerWebhook, Status: model.TaskSuccess, LogFile: svc.Config.Deploy.LogDir + "/4.log"}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "exit 99", model.ReportTypeAIReview)
	if outcome != stageOK || err != nil {
		t.Fatalf("unexpected outcome %v, %v", outcome, err)
	}
	var report model.Report
	if err := svc.DB.Where("task_id = ?", task.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.Status != model.ReportFailed || !strings.Contains(report.ErrorMessage, "commit") {
		t.Fatalf("unexpected report: %+v", report)
	}
}

// A report stage's capture_as value chooses the report type, and the capture
// script must declare the same type. The legacy "report" spelling still means
// an AI review so existing project configurations keep working.
func TestCaptureAsSelectsReportType(t *testing.T) {
	if got := model.NormalizeReportType("report"); got != model.ReportTypeAIReview {
		t.Fatalf("legacy capture_as must stay an AI review, got %q", got)
	}
	if got := model.NormalizeReportType(" AI_Review "); got != model.ReportTypeAIReview {
		t.Fatalf("capture_as should normalize to a report type, got %q", got)
	}
	if got := model.NormalizeReportType("perf_scan"); got != "" {
		t.Fatalf("unknown capture_as must be rejected, got %q", got)
	}

	svc := newTestService(t)
	project := model.Project{Name: "app", ProjectKey: "capture-as", AppDir: t.TempDir(), GitProvider: model.GitProviderGitHub, Branch: "main"}
	if err := svc.DB.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	task := model.DeployTask{ProjectID: project.ID, TriggerType: model.TriggerManual, Status: model.TaskSuccess, LogFile: svc.Config.Deploy.LogDir + "/5.log"}
	if err := svc.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	payload := `{"report_type":"perf_scan","status":"success","conclusion":"passed","issues":[],"markdown":""}`
	if _, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "printf '%s' '"+payload+"'", model.ReportTypeAIReview); err != nil {
		t.Fatal(err)
	}
	var report model.Report
	if err := svc.DB.Where("task_id = ?", task.ID).First(&report).Error; err != nil {
		t.Fatal(err)
	}
	if report.Type != model.ReportTypeAIReview {
		t.Fatalf("report must be stored under the stage's type, got %q", report.Type)
	}
	if report.Status != model.ReportFailed || !strings.Contains(report.ErrorMessage, "report_type") {
		t.Fatalf("a mismatched report_type must be rejected: %+v", report)
	}
}

// A notification link must survive being asked for again: a task with two
// outbound webhook stages would otherwise leave the first card's button dead.
func TestEnsureReportShareReusesLinkAndRotateInvalidatesIt(t *testing.T) {
	svc := newTestService(t)
	svc.Config.JWT.Secret = "test-secret"
	svc.Config.Server.PublicURL = "https://postdare.example.com/"
	report := model.Report{Type: model.ReportTypeAIReview, ProjectID: 1, TaskID: 1, Status: model.ReportSuccess}
	if err := svc.DB.Create(&report).Error; err != nil {
		t.Fatal(err)
	}

	_, first, err := svc.EnsureReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, "https://postdare.example.com/reports/1#token=") {
		t.Fatalf("unexpected share url %q", first)
	}
	_, second, err := svc.EnsureReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("ensure must reuse the live link:\n first=%s\nsecond=%s", first, second)
	}

	var stored model.Report
	if err := svc.DB.First(&stored, report.ID).Error; err != nil {
		t.Fatal(err)
	}
	token := strings.SplitN(first, "#token=", 2)[1]
	if strings.Contains(stored.ShareSalt, token) || stored.ShareTokenHash != HashReportToken(token) {
		t.Fatal("the raw share token must not be stored, only its digest")
	}

	_, rotated, rotatedURL, err := svc.RotateReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated == token || rotatedURL == first {
		t.Fatal("rotate must invalidate the previous link")
	}
	_, afterRotate, err := svc.EnsureReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRotate != rotatedURL {
		t.Fatal("ensure must hand out the link rotate just created")
	}
}

// Re-capturing a report keeps its share link alive, so a link already delivered
// to a chat channel still resolves.
func TestSaveReportPreservesShareState(t *testing.T) {
	svc := newTestService(t)
	svc.Config.JWT.Secret = "test-secret"
	report := model.Report{Type: model.ReportTypeAIReview, ProjectID: 1, TaskID: 7, Status: model.ReportSuccess}
	if err := svc.DB.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	_, url, err := svc.EnsureReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}

	recaptured := model.Report{Type: model.ReportTypeAIReview, ProjectID: 1, TaskID: 7, Status: model.ReportSuccess, Summary: "second run"}
	if err := svc.saveReport(context.Background(), &recaptured); err != nil {
		t.Fatal(err)
	}
	_, afterSave, err := svc.EnsureReportShare(context.Background(), report.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterSave != url {
		t.Fatalf("re-capture must not break the share link:\nbefore=%s\n after=%s", url, afterSave)
	}
}
