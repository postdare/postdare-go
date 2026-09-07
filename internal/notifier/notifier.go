package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/hellodeveye/postdare-go/internal/model"
)

const (
	TemplateDingTalkText     = "dingtalk_text"
	TemplateWeComText        = "wecom_text"
	TemplateFeishuText       = "feishu_text"
	TemplateFeishuReportCard = "feishu_report_card"
	TemplateGenericJSON      = "generic_json"
)

// Templates lists the outbound webhook payload formats a stage may select.
func Templates() []string {
	return []string{TemplateDingTalkText, TemplateWeComText, TemplateFeishuText, TemplateFeishuReportCard, TemplateGenericJSON}
}

// IsTemplate reports whether name is a payload format this package can render.
func IsTemplate(name string) bool {
	for _, template := range Templates() {
		if name == template {
			return true
		}
	}
	return false
}

const defaultMessageTemplate = `Postdare Go {{ .Scene }}
项目: {{ .Project.Name }}
Git: {{ .Task.GitProvider }}
触发: {{ .Task.TriggerType }}
分支: {{ .Task.Branch }}
commit: {{ .Task.CommitID }}
消息: {{ .Task.CommitMessage }}
阶段: {{ .Task.CurrentStage }}
状态: {{ .Task.Status }}
失败原因: {{ .Task.FailReason }}
任务ID: {{ .Task.ID }}
耗时: {{ .Duration }}`

type Notifier struct {
	Client *http.Client
	Logger *zap.Logger
}

func New(logger *zap.Logger) *Notifier {
	return &Notifier{
		Client: &http.Client{Timeout: 10 * time.Second},
		Logger: logger,
	}
}

type MessageContext struct {
	Project  model.Project
	Task     model.DeployTask
	Scene    string
	Duration string
}

func (n *Notifier) SendOutboundWebhook(project model.Project, task model.DeployTask, cfg model.OutboundWebhookStageConfig) error {
	return n.SendOutboundWebhookWithReport(project, task, cfg, nil, "")
}

func (n *Notifier) SendOutboundWebhookWithReport(project model.Project, task model.DeployTask, cfg model.OutboundWebhookStageConfig, report *model.Report, reportURL string) error {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil
	}
	content, err := renderMessage(project, task, cfg.MessageTemplate)
	if err != nil {
		return err
	}
	raw, err := renderPayloadWithReport(cfg, content, project, task, report, reportURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.Client.Do(req)
	if err != nil {
		n.Logger.Warn("outbound webhook failed", zap.Uint64("task_id", task.ID), zap.String("webhook", maskedURL(cfg.URL)), zap.Error(err))
		return err
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 300 {
		err := fmt.Errorf("outbound webhook returned status %d", resp.StatusCode)
		n.Logger.Warn("outbound webhook failed", zap.Uint64("task_id", task.ID), zap.String("webhook", maskedURL(cfg.URL)), zap.Error(err))
		return err
	}
	if readErr != nil {
		err := fmt.Errorf("read outbound webhook response: %w", readErr)
		n.Logger.Warn("outbound webhook failed", zap.Uint64("task_id", task.ID), zap.String("webhook", maskedURL(cfg.URL)), zap.Error(err))
		return err
	}
	if err := validateWebhookResponse(cfg.Template, respBody); err != nil {
		n.Logger.Warn("outbound webhook failed", zap.Uint64("task_id", task.ID), zap.String("webhook", maskedURL(cfg.URL)), zap.Error(err))
		return err
	}
	return nil
}

func renderMessage(project model.Project, task model.DeployTask, messageTemplate string) (string, error) {
	if strings.TrimSpace(messageTemplate) == "" {
		messageTemplate = defaultMessageTemplate
	}
	tpl, err := template.New("outbound_webhook_message").Parse(messageTemplate)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, MessageContext{
		Project:  project,
		Task:     task,
		Scene:    taskScene(task),
		Duration: taskDuration(task),
	}); err != nil {
		return "", err
	}
	return out.String(), nil
}

func renderPayload(cfg model.OutboundWebhookStageConfig, content string) ([]byte, error) {
	return renderPayloadWithReport(cfg, content, model.Project{}, model.DeployTask{}, nil, "")
}

func renderPayloadWithReport(cfg model.OutboundWebhookStageConfig, content string, project model.Project, task model.DeployTask, report *model.Report, reportURL string) ([]byte, error) {
	templateName := outboundTemplateName(cfg.Template)
	if templateName == TemplateFeishuReportCard {
		return json.Marshal(feishuReportCard(project, task, report, reportURL))
	}
	if templateName == TemplateGenericJSON {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(content), &raw); err != nil {
			return nil, fmt.Errorf("generic_json message_template must render valid JSON: %w", err)
		}
		return raw, nil
	}
	var body map[string]interface{}
	switch templateName {
	case TemplateFeishuText:
		body = feishuText(content)
	case TemplateDingTalkText, TemplateWeComText:
		body = dingtalkText(content)
	default:
		return nil, fmt.Errorf("unsupported outbound webhook template %q", templateName)
	}
	return json.Marshal(body)
}

func outboundTemplateName(templateName string) string {
	templateName = strings.TrimSpace(templateName)
	if templateName == "" {
		return TemplateDingTalkText
	}
	return templateName
}

func validateWebhookResponse(templateName string, body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil
	}
	var decoded map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}
	switch outboundTemplateName(templateName) {
	case TemplateFeishuText, TemplateFeishuReportCard:
		if code, ok := responseCode(decoded, "code", "StatusCode"); ok && code != 0 {
			return fmt.Errorf("outbound webhook business error %s: %s", formatResponseCode(code), responseMessage(decoded))
		}
	case TemplateDingTalkText, TemplateWeComText:
		if code, ok := responseCode(decoded, "errcode"); ok && code != 0 {
			return fmt.Errorf("outbound webhook business error %s: %s", formatResponseCode(code), responseMessage(decoded))
		}
	}
	return nil
}

func feishuReportCard(project model.Project, task model.DeployTask, report *model.Report, reportURL string) map[string]interface{} {
	status := "报告失败"
	conclusion := "failed"
	summary := "AI 审查报告未生成，请查看部署阶段日志。"
	counts := map[string]int{}
	for _, severity := range model.Severities() {
		counts[severity] = 0
	}
	issues := []model.ReportIssue{}
	if report != nil {
		status = report.Status
		conclusion = report.Conclusion
		summary = truncateRunes(report.Summary, 240)
		if summary == "" && report.ErrorMessage != "" {
			summary = truncateRunes(report.ErrorMessage, 240)
		}
		issues = report.Issues
		for _, issue := range issues {
			counts[issue.Severity]++
		}
	}
	headerColor := "grey"
	if report != nil && report.Status == model.ReportSuccess {
		switch {
		case counts[model.SeverityHigh] > 0:
			headerColor = "red"
		case len(issues) > 0:
			headerColor = "orange"
		default:
			headerColor = "green"
		}
	}
	elements := []interface{}{
		map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": fmt.Sprintf("**部署状态** %s\n**项目** %s\n**分支** %s\n**Commit** `%s`", task.Status, project.Name, task.Branch, shortCommit(task.CommitID))}},
		map[string]interface{}{"tag": "hr"},
		map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": fmt.Sprintf("**报告类型** AI Review\n**审查结论** %s\n**风险** 高 %d · 中 %d · 低 %d", conclusion, counts[model.SeverityHigh], counts[model.SeverityMedium], counts[model.SeverityLow])}},
		map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": summary}},
	}
	highlights := make([]string, 0, 3)
	// model.Severities is ordered most serious first, so the card highlights the
	// three issues that matter most.
	for _, severity := range model.Severities() {
		for _, issue := range issues {
			if issue.Severity == severity && len(highlights) < 3 {
				highlights = append(highlights, fmt.Sprintf("• **%s** %s", strings.ToUpper(issue.Severity), issue.Title))
			}
		}
	}
	if len(highlights) > 0 {
		elements = append(elements, map[string]interface{}{"tag": "div", "text": map[string]string{"tag": "lark_md", "content": strings.Join(highlights, "\n")}})
	}
	if reportURL != "" {
		elements = append(elements, map[string]interface{}{"tag": "action", "actions": []interface{}{map[string]interface{}{"tag": "button", "type": "primary", "text": map[string]string{"tag": "plain_text", "content": "查看完整报告"}, "url": reportURL}}})
	}
	return map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header":   map[string]interface{}{"template": headerColor, "title": map[string]string{"tag": "plain_text", "content": fmt.Sprintf("%s · AI 审查 %s", project.Name, status)}},
			"elements": elements,
		},
	}
}

func truncateRunes(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max-1]) + "…"
}

func shortCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 8 {
		return value[:8]
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func responseCode(decoded map[string]interface{}, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := decoded[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case json.Number:
			n, err := v.Float64()
			return n, err == nil
		case float64:
			return v, true
		case int:
			return float64(v), true
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return n, err == nil
		}
	}
	return 0, false
}

func formatResponseCode(code float64) string {
	if code == float64(int64(code)) {
		return strconv.FormatInt(int64(code), 10)
	}
	return strconv.FormatFloat(code, 'f', -1, 64)
}

func responseMessage(decoded map[string]interface{}) string {
	for _, key := range []string{"msg", "errmsg", "message", "StatusMessage"} {
		if value, ok := decoded[key]; ok {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return "no message"
}

func dingtalkText(content string) map[string]interface{} {
	return map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
	}
}

func feishuText(content string) map[string]interface{} {
	return map[string]interface{}{
		"msg_type": "text",
		"content": map[string]string{
			"text": content,
		},
	}
}

func taskScene(task model.DeployTask) string {
	switch task.Status {
	case model.TaskSuccess:
		return "部署成功"
	case model.TaskFailed:
		return "部署失败"
	case model.TaskRollbacked:
		return "回滚成功"
	case model.TaskCanceled:
		return "部署取消"
	default:
		return "部署进行中"
	}
}

func taskDuration(task model.DeployTask) string {
	if task.StartedAt == nil {
		return ""
	}
	end := time.Now()
	if task.FinishedAt != nil {
		end = *task.FinishedAt
	}
	return end.Sub(*task.StartedAt).Round(time.Second).String()
}

func maskedURL(raw string) string {
	if len(raw) <= 16 {
		return "******"
	}
	return raw[:12] + "******"
}
