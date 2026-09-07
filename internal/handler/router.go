package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"github.com/hellodeveye/postdare-go/internal/util"
	"gorm.io/gorm"
)

type Handler struct {
	DB         *gorm.DB
	Config     *config.Config
	Service    *service.Service
	Hub        *sse.Hub
	AppVersion string
}

func RegisterRoutes(r *gin.Engine, h *Handler) {
	api := r.Group("/api/v1")
	api.GET("/version", h.GetVersion)
	api.POST("/auth/login", h.Login)
	api.POST("/webhooks/gitee/:project_key", h.HandleGiteeWebhook)
	api.POST("/webhooks/github/:project_key", h.HandleGitHubWebhook)
	api.GET("/public/reports/:report_id", h.GetPublicReport)

	secured := api.Group("")
	secured.Use(middleware.Auth(h.Config))
	secured.GET("/auth/me", h.Me)
	secured.POST("/auth/logout", h.Logout)
	secured.PUT("/auth/password", h.ChangePassword)

	secured.Use(h.RequirePasswordReady)

	secured.GET("/projects", h.ListProjects)
	secured.POST("/projects", h.CreateProject)
	secured.GET("/projects/:project_id", h.GetProject)
	secured.PATCH("/projects/:project_id", h.UpdateProject)
	secured.DELETE("/projects/:project_id", h.DeleteProject)
	secured.POST("/projects/:project_id/deploy-tasks", h.CreateProjectDeployTask)
	secured.POST("/projects/:project_id/rollback-tasks", h.CreateProjectRollbackTask)
	secured.GET("/projects/:project_id/app-logs", h.GetProjectAppLogs)
	secured.GET("/projects/:project_id/app-logs/stream", h.StreamProjectAppLogs)

	secured.GET("/deploy-tasks", h.ListDeployTasks)
	secured.GET("/deploy-tasks/:task_id", h.GetDeployTask)
	secured.GET("/deploy-tasks/:task_id/stages", h.GetDeployTaskStages)
	secured.GET("/deploy-tasks/:task_id/logs", h.GetDeployTaskLogs)
	secured.GET("/deploy-tasks/:task_id/logs/stream", h.StreamDeployTaskLogs)
	secured.POST("/deploy-tasks/:task_id/cancel", h.CancelDeployTask)
	secured.GET("/deploy-tasks/:task_id/analysis", h.AnalyzeDeployTask)
	secured.GET("/deploy-tasks/:task_id/reports", h.ListDeployTaskReports)
	secured.GET("/reports/:report_id", h.GetReport)
	secured.POST("/reports/:report_id/share", h.ShareReport)
	secured.DELETE("/reports/:report_id/share", h.RevokeReportShare)

	secured.GET("/webhook-events", h.ListWebhookEvents)
	secured.GET("/webhook-events/:event_id", h.GetWebhookEvent)

	secured.GET("/dashboard/summary", h.DashboardSummary)
	secured.GET("/dashboard/recent-deploy-tasks", h.DashboardRecentDeployTasks)

	secured.GET("/settings", h.GetSettings)
	secured.PATCH("/settings", h.PatchSettings)
}

func parseUintParam(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		util.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid id", gin.H{"param": name})
		return 0, false
	}
	return id, true
}
