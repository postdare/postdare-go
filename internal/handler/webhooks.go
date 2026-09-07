package handler

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/util"
	"github.com/hellodeveye/postdare-go/internal/webhook"
)

func (h *Handler) ListWebhookEvents(c *gin.Context) {
	page, pageSize, offset := util.ParsePagination(c)
	query := h.DB.Model(&model.WebhookEvent{})
	if provider := c.Query("provider"); provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if projectID := c.Query("project_id"); projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	var total int64
	_ = query.Count(&total).Error
	var events []model.WebhookEvent
	if err := query.Order("created_at desc").Limit(pageSize).Offset(offset).Find(&events).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "WEBHOOK_EVENT_LIST_FAILED", "Failed to list webhook events", nil)
		return
	}
	util.List(c, events, util.Pagination{Page: page, PageSize: pageSize, Total: total})
}

func (h *Handler) GetWebhookEvent(c *gin.Context) {
	id, ok := parseUintParam(c, "event_id")
	if !ok {
		return
	}
	var event model.WebhookEvent
	if err := h.DB.First(&event, id).Error; err != nil {
		util.Error(c, http.StatusNotFound, "WEBHOOK_EVENT_NOT_FOUND", "Webhook event not found", nil)
		return
	}
	util.OK(c, event)
}

func (h *Handler) HandleGiteeWebhook(c *gin.Context) {
	h.handleWebhook(c, model.GitProviderGitee, webhook.GiteeWebhookParser{})
}

func (h *Handler) HandleGitHubWebhook(c *gin.Context) {
	h.handleWebhook(c, model.GitProviderGitHub, webhook.GitHubWebhookParser{})
}

func (h *Handler) handleWebhook(c *gin.Context, provider string, parser webhook.WebhookParser) {
	projectKey := c.Param("project_key")
	body, err := c.GetRawData()
	if err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Failed to read webhook payload", nil)
		return
	}
	var project model.Project
	projectErr := h.DB.Where("project_key = ?", projectKey).First(&project).Error
	ev, parseErr := parser.Parse(c.Request.Header, body)
	if parseErr != nil {
		ev = &webhook.Event{Provider: webhook.GitProvider(provider), RawPayload: body}
	}
	signatureValid := parser.VerifySignature(project.WebhookSecret, c.Request.Header, body)
	if provider == model.GitProviderGitee && project.WebhookSecret != "" {
		signatureValid = signatureValid || subtle.ConstantTimeCompare([]byte(c.Query("token")), []byte(project.WebhookSecret)) == 1
	}

	dbEvent := model.WebhookEvent{
		Provider:       provider,
		ProjectKey:     projectKey,
		EventType:      ev.EventType,
		Branch:         ev.Branch,
		CommitID:       ev.CommitID,
		BeforeCommitID: ev.BeforeCommitID,
		CommitMessage:  ev.CommitMessage,
		CommitAuthor:   ev.CommitAuthor,
		DeliveryID:     ev.DeliveryID,
		SignatureValid: signatureValid,
		RawPayload:     json.RawMessage(body),
	}
	if projectErr == nil {
		dbEvent.ProjectID = &project.ID
	}
	ignored := ""
	if projectErr != nil {
		ignored = "project not found"
	} else if project.GitProvider != provider {
		ignored = "project git_provider mismatch"
	} else if !signatureValid {
		ignored = "invalid webhook signature or token"
	} else if ev.EventType != "" && ev.EventType != "push" {
		ignored = "unsupported event type"
	} else if !project.AutoDeployEnabled {
		ignored = "auto deploy disabled"
	} else if ev.Branch != project.Branch {
		ignored = "branch mismatch"
	}
	if ignored != "" {
		dbEvent.IgnoredReason = ignored
		_ = h.DB.Create(&dbEvent).Error
		util.Accepted(c, gin.H{"handled": false, "ignored_reason": ignored})
		return
	}
	task, err := h.Service.CreateDeployTask(c.Request.Context(), project, model.TriggerWebhook, ev)
	if err != nil {
		dbEvent.IgnoredReason = err.Error()
		_ = h.DB.Create(&dbEvent).Error
		h.deployTaskError(c, err)
		return
	}
	dbEvent.Handled = true
	_ = h.DB.Create(&dbEvent).Error
	util.Accepted(c, gin.H{"handled": true, "task": maskTask(*task)})
}

func (h *Handler) allowMCPMutation(c *gin.Context) bool {
	if !middleware.IsMCP(c) {
		return true
	}
	if !h.Config.MCP.AllowMutationTools {
		util.Error(c, http.StatusForbidden, "MCP_MUTATION_DISABLED", "MCP mutation tools are disabled", nil)
		return false
	}
	var payload struct {
		Confirm bool `json:"confirm"`
	}
	if c.Request.Body != nil {
		raw, _ := c.GetRawData()
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))
		_ = json.Unmarshal(raw, &payload)
	}
	if !payload.Confirm {
		util.Error(c, http.StatusUnprocessableEntity, "CONFIRM_REQUIRED", "confirm=true is required for MCP mutation tools", nil)
		return false
	}
	return true
}
