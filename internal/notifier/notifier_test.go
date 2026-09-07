package notifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/hellodeveye/postdare-go/internal/model"
)

func TestSendOutboundWebhookRendersFeishuTextTemplate(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	project := model.Project{Name: "app"}
	task := model.DeployTask{ID: 42, Status: model.TaskFailed, CurrentStage: "build", FailReason: "exit status 1"}
	err := New(zap.NewNop()).SendOutboundWebhook(project, task, model.OutboundWebhookStageConfig{
		URL:             server.URL,
		Template:        TemplateFeishuText,
		MessageTemplate: "项目={{ .Project.Name }} 状态={{ .Task.Status }} 阶段={{ .Task.CurrentStage }}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["msg_type"] != "text" {
		t.Fatalf("expected feishu text payload, got %+v", got)
	}
	content, ok := got["content"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected content object, got %+v", got["content"])
	}
	text, _ := content["text"].(string)
	if !strings.Contains(text, "项目=app") || !strings.Contains(text, "状态=failed") || !strings.Contains(text, "阶段=build") {
		t.Fatalf("unexpected rendered text: %q", text)
	}
}

func TestFeishuReportCardUsesRiskColorSummaryAndLink(t *testing.T) {
	project := model.Project{Name: "xianhu"}
	task := model.DeployTask{ID: 9, Status: model.TaskSuccess, Branch: "release", CommitID: "1234567890abcdef"}
	report := &model.Report{Status: model.ReportSuccess, Conclusion: "issues_found", Summary: strings.Repeat("审", 260), Issues: []model.ReportIssue{
		{Severity: "high", Title: "SQL injection"}, {Severity: "medium", Title: "Missing timeout"}, {Severity: "low", Title: "Weak message"}, {Severity: "low", Title: "Fourth"},
	}}
	raw, err := renderPayloadWithReport(model.OutboundWebhookStageConfig{Template: TemplateFeishuReportCard}, "", project, task, report, "https://go.postdare.com/reports/1#token=secret")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["msg_type"] != "interactive" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	card := payload["card"].(map[string]interface{})
	header := card["header"].(map[string]interface{})
	if header["template"] != "red" {
		t.Fatalf("expected red header, got %+v", header)
	}
	if !strings.Contains(string(raw), "查看完整报告") || !strings.Contains(string(raw), "reports/1#token=secret") {
		t.Fatalf("missing report action: %s", raw)
	}
	if strings.Contains(string(raw), "Fourth") {
		t.Fatal("card includes more than three highlighted issues")
	}
}

func TestSendOutboundWebhookDetectsFeishuBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":9499,"msg":"Bad Request","StatusCode":9499,"StatusMessage":"Bad Request"}`))
	}))
	defer server.Close()

	err := New(zap.NewNop()).SendOutboundWebhook(model.Project{Name: "app"}, model.DeployTask{ID: 42}, model.OutboundWebhookStageConfig{
		URL:      server.URL,
		Template: TemplateFeishuText,
	})
	if err == nil || !strings.Contains(err.Error(), "9499") {
		t.Fatalf("expected Feishu business error, got %v", err)
	}
}

func TestSendOutboundWebhookDetectsDingTalkBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"keywords not in content"}`))
	}))
	defer server.Close()

	err := New(zap.NewNop()).SendOutboundWebhook(model.Project{Name: "app"}, model.DeployTask{ID: 42}, model.OutboundWebhookStageConfig{
		URL:      server.URL,
		Template: TemplateDingTalkText,
	})
	if err == nil || !strings.Contains(err.Error(), "310000") {
		t.Fatalf("expected DingTalk business error, got %v", err)
	}
}
