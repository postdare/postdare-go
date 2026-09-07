package model

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	GitProviderGitee  = "gitee"
	GitProviderGitHub = "github"

	TriggerManual   = "manual"
	TriggerWebhook  = "webhook"
	TriggerRollback = "rollback"
	TriggerMCP      = "mcp"

	TaskPending    = "pending"
	TaskRunning    = "running"
	TaskSuccess    = "success"
	TaskFailed     = "failed"
	TaskCanceled   = "canceled"
	TaskRollbacked = "rollbacked"

	StagePending = "pending"
	StageRunning = "running"
	StageSuccess = "success"
	StageFailed  = "failed"
	StageSkipped = "skipped"

	ReportTypeAIReview = "ai_review"
	ReportSuccess      = "success"
	ReportFailed       = "failed"
	ReportSkipped      = "skipped"

	// CaptureAsReportLegacy is the capture_as value shipped by the first release
	// of the report pipeline, when "report" implicitly meant an AI review. It is
	// still accepted so existing project configurations keep working.
	CaptureAsReportLegacy = "report"

	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"

	ProjectStageTypeCommand         = "command"
	ProjectStageTypeHealthCheck     = "health_check"
	ProjectStageTypeOutboundWebhook = "outbound_webhook"

	ProjectStageRunWhenSuccess = "success"
	ProjectStageRunWhenFailed  = "failed"
	ProjectStageRunWhenAlways  = "always"
)

type User struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	Username           string    `gorm:"size:100;uniqueIndex;not null" json:"username"`
	PasswordHash       string    `gorm:"size:255;not null" json:"-"`
	Role               string    `gorm:"size:50;not null;default:admin" json:"role"`
	MustChangePassword bool      `gorm:"not null;default:false" json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type Project struct {
	ID                uint64         `gorm:"primaryKey" json:"id"`
	Name              string         `gorm:"size:100;not null" json:"name"`
	ProjectKey        string         `gorm:"size:100;uniqueIndex;not null" json:"project_key"`
	GitProvider       string         `gorm:"size:50;not null;default:gitee" json:"git_provider"`
	Branch            string         `gorm:"size:100;not null;default:main" json:"branch"`
	AppDir            string         `gorm:"size:500;not null" json:"app_dir"`
	RollbackCmd       string         `gorm:"type:text" json:"rollback_cmd"`
	Stages            []ProjectStage `gorm:"column:deploy_stages;serializer:json;type:json" json:"deploy_stages"`
	AppLogPath        string         `gorm:"size:500" json:"app_log_path"`
	WebhookSecret     string         `gorm:"size:255" json:"webhook_secret,omitempty"`
	AutoDeployEnabled bool           `gorm:"not null;default:false" json:"auto_deploy_enabled"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// ProjectStage is one entry in a project's ordered, user-defined deploy pipeline.
// Stages run in slice order; disabled stages are skipped, and ContinueOnError
// allows a failed command to be recorded without aborting the task.
type ProjectStage struct {
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Enabled         bool            `json:"enabled"`
	RunWhen         string          `json:"run_when,omitempty"`
	ContinueOnError bool            `json:"continue_on_error,omitempty"`
	Config          json.RawMessage `json:"config,omitempty"`
}

type CommandStageConfig struct {
	Command   string `json:"command"`
	CaptureAs string `json:"capture_as,omitempty"`
}

type HealthCheckStageConfig struct {
	URL string `json:"url,omitempty"`
}

type OutboundWebhookStageConfig struct {
	URL             string `json:"url,omitempty"`
	Template        string `json:"template"`
	MessageTemplate string `json:"message_template,omitempty"`
}

type DeployTask struct {
	ID             uint64            `gorm:"primaryKey" json:"id"`
	ProjectID      uint64            `gorm:"not null;index:idx_deploy_tasks_project_id" json:"project_id"`
	Project        *Project          `json:"project,omitempty"`
	Stages         []DeployTaskStage `gorm:"foreignKey:TaskID" json:"stages,omitempty"`
	TriggerType    string            `gorm:"size:50;not null" json:"trigger_type"`
	GitProvider    string            `gorm:"size:50" json:"git_provider"`
	Branch         string            `gorm:"size:100" json:"branch"`
	CommitID       string            `gorm:"size:100" json:"commit_id"`
	BeforeCommitID string            `gorm:"size:100" json:"before_commit_id"`
	CommitMessage  string            `gorm:"type:text" json:"commit_message"`
	CommitAuthor   string            `gorm:"size:100" json:"commit_author"`
	Status         string            `gorm:"size:50;not null;index:idx_deploy_tasks_status" json:"status"`
	CurrentStage   string            `gorm:"size:100" json:"current_stage"`
	FailReason     string            `gorm:"type:text" json:"fail_reason"`
	LogFile        string            `gorm:"size:500" json:"log_file"`
	StartedAt      *time.Time        `json:"started_at"`
	FinishedAt     *time.Time        `json:"finished_at"`
	CreatedAt      time.Time         `gorm:"index:idx_deploy_tasks_created_at" json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type DeployTaskStage struct {
	ID           uint64     `gorm:"primaryKey" json:"id"`
	TaskID       uint64     `gorm:"not null;index:idx_deploy_task_stages_task_id" json:"task_id"`
	Name         string     `gorm:"size:100;not null" json:"name"`
	Status       string     `gorm:"size:50;not null" json:"status"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	ExitCode     *int       `json:"exit_code"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type WebhookEvent struct {
	ID             uint64          `gorm:"primaryKey" json:"id"`
	Provider       string          `gorm:"size:50;not null;index:idx_webhook_events_provider" json:"provider"`
	ProjectID      *uint64         `gorm:"index:idx_webhook_events_project_id" json:"project_id"`
	ProjectKey     string          `gorm:"size:100" json:"project_key"`
	EventType      string          `gorm:"size:100" json:"event_type"`
	Branch         string          `gorm:"size:100" json:"branch"`
	CommitID       string          `gorm:"size:100" json:"commit_id"`
	BeforeCommitID string          `gorm:"size:100" json:"before_commit_id"`
	CommitMessage  string          `gorm:"type:text" json:"commit_message"`
	CommitAuthor   string          `gorm:"size:100" json:"commit_author"`
	DeliveryID     string          `gorm:"size:255" json:"delivery_id"`
	SignatureValid bool            `gorm:"not null;default:false" json:"signature_valid"`
	Handled        bool            `gorm:"not null;default:false" json:"handled"`
	IgnoredReason  string          `gorm:"type:text" json:"ignored_reason"`
	RawPayload     json.RawMessage `gorm:"type:json" json:"raw_payload,omitempty"`
	CreatedAt      time.Time       `gorm:"index:idx_webhook_events_created_at" json:"created_at"`
}

// ReportTypes lists the report kinds a command stage may capture. A stage's
// capture_as value names the type directly, so adding a kind here is all the
// capture pipeline needs to accept, store and serve it.
func ReportTypes() []string {
	return []string{ReportTypeAIReview}
}

// NormalizeReportType maps a capture_as value to a report type, returning ""
// when the stage captures nothing or names an unknown kind.
func NormalizeReportType(captureAs string) string {
	value := strings.ToLower(strings.TrimSpace(captureAs))
	if value == CaptureAsReportLegacy {
		return ReportTypeAIReview
	}
	for _, reportType := range ReportTypes() {
		if value == reportType {
			return reportType
		}
	}
	return ""
}

// Severities lists issue severities from most to least serious. Callers rely on
// the ordering when ranking issues for a notification.
func Severities() []string {
	return []string{SeverityHigh, SeverityMedium, SeverityLow}
}

// NormalizeSeverity coerces a severity reported by a capture script to a known
// value, defaulting to the least serious one.
func NormalizeSeverity(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, severity := range Severities() {
		if normalized == severity {
			return severity
		}
	}
	return SeverityLow
}

// IsSeverity reports whether value is a severity a capture script may use.
func IsSeverity(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, severity := range Severities() {
		if normalized == severity {
			return true
		}
	}
	return false
}

type ReportIssue struct {
	Severity   string `json:"severity"`
	Title      string `json:"title"`
	Location   string `json:"location,omitempty"`
	Trigger    string `json:"trigger,omitempty"`
	Impact     string `json:"impact,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Report stores a structured artifact produced by a deploy stage. The raw share
// token is never stored or serialized: ShareSalt plus the server secret derive
// it on demand and ShareTokenHash verifies it.
type Report struct {
	ID             uint64        `gorm:"primaryKey" json:"id"`
	Type           string        `gorm:"size:50;not null;uniqueIndex:idx_reports_task_type" json:"type"`
	ProjectID      uint64        `gorm:"not null;index:idx_reports_project_id" json:"project_id"`
	TaskID         uint64        `gorm:"not null;uniqueIndex:idx_reports_task_type;index:idx_reports_task_id" json:"task_id"`
	CommitID       string        `gorm:"size:100" json:"commit_id"`
	BeforeCommitID string        `gorm:"size:100" json:"before_commit_id"`
	Status         string        `gorm:"size:50;not null" json:"status"`
	Conclusion     string        `gorm:"size:100" json:"conclusion"`
	Summary        string        `gorm:"type:text" json:"summary"`
	Issues         []ReportIssue `gorm:"serializer:json;type:json" json:"issues"`
	Markdown       string        `gorm:"type:longtext" json:"markdown"`
	ErrorMessage   string        `gorm:"type:text" json:"error_message,omitempty"`
	ShareSalt      string        `gorm:"size:64" json:"-"`
	ShareTokenHash string        `gorm:"size:64" json:"-"`
	ShareEnabled   bool          `gorm:"not null;default:false" json:"share_enabled"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type Setting struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"size:120;uniqueIndex;not null" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
