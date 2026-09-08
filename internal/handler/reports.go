package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/util"
	"gorm.io/gorm"
)

type reportResponse struct {
	ID             uint64              `json:"id"`
	Type           string              `json:"type"`
	ProjectID      uint64              `json:"project_id"`
	ProjectName    string              `json:"project_name"`
	TaskID         uint64              `json:"task_id"`
	TriggerType    string              `json:"trigger_type"`
	Branch         string              `json:"branch"`
	CommitID       string              `json:"commit_id"`
	BeforeCommitID string              `json:"before_commit_id"`
	DeployStatus   string              `json:"deploy_status"`
	Status         string              `json:"status"`
	Conclusion     string              `json:"conclusion"`
	Summary        string              `json:"summary"`
	Issues         []model.ReportIssue `json:"issues"`
	Markdown       string              `json:"markdown"`
	ErrorMessage   string              `json:"error_message,omitempty"`
	ShareEnabled   bool                `json:"share_enabled"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

func (h *Handler) ListDeployTaskReports(c *gin.Context) {
	taskID, ok := parseUintParam(c, "task_id")
	if !ok {
		return
	}
	var count int64
	if err := h.DB.Model(&model.DeployTask{}).Where("id = ?", taskID).Count(&count).Error; err != nil || count == 0 {
		util.Error(c, http.StatusNotFound, "DEPLOY_TASK_NOT_FOUND", "Deploy task not found", nil)
		return
	}
	var reports []model.Report
	if err := h.DB.Where("task_id = ?", taskID).Order("id asc").Find(&reports).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "REPORT_LIST_FAILED", "Failed to list reports", nil)
		return
	}
	out := make([]reportResponse, 0, len(reports))
	for _, report := range reports {
		response, err := h.reportResponse(report)
		if err == nil {
			out = append(out, response)
		}
	}
	c.Header("Cache-Control", "no-store")
	util.OK(c, out)
}

func (h *Handler) GetReport(c *gin.Context) {
	report, ok := h.loadReport(c)
	if !ok {
		return
	}
	response, err := h.reportResponse(report)
	if err != nil {
		util.Error(c, http.StatusInternalServerError, "REPORT_LOAD_FAILED", "Failed to load report", nil)
		return
	}
	c.Header("Cache-Control", "no-store")
	util.OK(c, response)
}

func (h *Handler) ShareReport(c *gin.Context) {
	id, ok := parseUintParam(c, "report_id")
	if !ok {
		return
	}
	report, token, url, err := h.Service.RotateReportShare(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrReportNotFound) {
			util.Error(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Report not found", nil)
			return
		}
		util.Error(c, http.StatusInternalServerError, "REPORT_SHARE_FAILED", "Failed to share report", nil)
		return
	}
	c.Header("Cache-Control", "no-store")
	util.OK(c, gin.H{"report_id": report.ID, "token": token, "url": url})
}

func (h *Handler) RevokeReportShare(c *gin.Context) {
	id, ok := parseUintParam(c, "report_id")
	if !ok {
		return
	}
	if err := h.Service.RevokeReportShare(c.Request.Context(), id); err != nil {
		if errors.Is(err, service.ErrReportNotFound) {
			util.Error(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Report not found", nil)
			return
		}
		util.Error(c, http.StatusInternalServerError, "REPORT_SHARE_REVOKE_FAILED", "Failed to revoke report share", nil)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func (h *Handler) GetPublicReport(c *gin.Context) {
	id, ok := parseUintParam(c, "report_id")
	if !ok {
		return
	}
	token := strings.TrimSpace(c.GetHeader("X-Report-Token"))
	if token == "" {
		util.Error(c, http.StatusUnauthorized, "REPORT_TOKEN_REQUIRED", "Report token is required", nil)
		return
	}
	var report model.Report
	if err := h.DB.Where("id = ? AND share_enabled = ? AND share_token_hash = ?", id, true, service.HashReportToken(token)).First(&report).Error; err != nil {
		util.Error(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Report not found", nil)
		return
	}
	response, err := h.reportResponse(report)
	if err != nil {
		util.Error(c, http.StatusInternalServerError, "REPORT_LOAD_FAILED", "Failed to load report", nil)
		return
	}
	// Diff excerpts are served here too: a shared report exists so a reader without
	// repo access can check a finding, and the excerpt is what makes that possible.
	// The trade is deliberate -- whoever holds the share link reads the reviewed
	// source with it -- so revoke a link that has spread rather than one that has not.
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	util.OK(c, response)
}

func (h *Handler) loadReport(c *gin.Context) (model.Report, bool) {
	id, ok := parseUintParam(c, "report_id")
	if !ok {
		return model.Report{}, false
	}
	var report model.Report
	if err := h.DB.First(&report, id).Error; err != nil {
		util.Error(c, http.StatusNotFound, "REPORT_NOT_FOUND", "Report not found", nil)
		return model.Report{}, false
	}
	return report, true
}

func (h *Handler) reportResponse(report model.Report) (reportResponse, error) {
	var task model.DeployTask
	if err := h.DB.First(&task, report.TaskID).Error; err != nil {
		return reportResponse{}, err
	}
	var project model.Project
	if err := h.DB.Select("id", "name").First(&project, report.ProjectID).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return reportResponse{}, err
	}
	// Rows written before the capture path seeded the list hold JSON null. Serve
	// them as an empty array so a report that failed or was skipped still renders.
	issues := report.Issues
	if issues == nil {
		issues = []model.ReportIssue{}
	}
	return reportResponse{
		ID: report.ID, Type: report.Type, ProjectID: report.ProjectID, ProjectName: project.Name,
		TaskID: report.TaskID, TriggerType: task.TriggerType, Branch: task.Branch,
		CommitID: report.CommitID, BeforeCommitID: report.BeforeCommitID, DeployStatus: task.Status,
		Status: report.Status, Conclusion: report.Conclusion, Summary: report.Summary,
		Issues: issues, Markdown: report.Markdown, ErrorMessage: report.ErrorMessage,
		ShareEnabled: report.ShareEnabled, CreatedAt: report.CreatedAt, UpdatedAt: report.UpdatedAt,
	}, nil
}
