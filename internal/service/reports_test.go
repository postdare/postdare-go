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
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "printf '%s' '"+payload+"'")
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
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "printf not-json")
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
	_, _ = svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "exit 99")
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
	outcome, err := svc.runReportCommandStage(context.Background(), project, &task, "ai_review", "exit 99")
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
