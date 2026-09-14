package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/mcp"
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

	secured.GET("/boards", h.ListBoards)
	secured.POST("/boards", h.CreateBoard)
	secured.GET("/boards/:board_id", h.GetBoard)
	secured.PATCH("/boards/:board_id", h.UpdateBoard)
	secured.DELETE("/boards/:board_id", h.DeleteBoard)
	secured.GET("/boards/:board_id/issues", h.ListBoardIssues)
	secured.POST("/boards/:board_id/issues", h.CreateBoardIssue)
	secured.GET("/boards/:board_id/labels", h.ListBoardLabels)
	secured.GET("/boards/:board_id/stream", h.StreamBoard)
	secured.GET("/issues/meta", h.IssueMetadata)
	secured.GET("/issues/:issue_id", h.GetIssue)
	secured.PATCH("/issues/:issue_id", h.UpdateIssue)
	secured.DELETE("/issues/:issue_id", h.DeleteIssue)
	secured.POST("/issues/:issue_id/move", h.MoveIssue)
	secured.GET("/issues/:issue_id/comments", h.ListIssueComments)
	secured.POST("/issues/:issue_id/comments", h.CreateIssueComment)
	secured.PATCH("/issue-comments/:comment_id", h.UpdateIssueComment)
	secured.DELETE("/issue-comments/:comment_id", h.DeleteIssueComment)
	secured.GET("/users", h.ListBoardUsers)
	secured.POST("/attachments", h.UploadAttachment)
	secured.GET("/attachments/:attachment_id", h.GetAttachment)

	secured.GET("/webhook-events", h.ListWebhookEvents)
	secured.GET("/webhook-events/:event_id", h.GetWebhookEvent)

	secured.GET("/dashboard/summary", h.DashboardSummary)
	secured.GET("/dashboard/recent-deploy-tasks", h.DashboardRecentDeployTasks)

	secured.GET("/settings", h.GetSettings)
	secured.PATCH("/settings", h.PatchSettings)

	registerMCPEndpoint(r, h)
}

// registerMCPEndpoint mounts the MCP Streamable HTTP transport at /mcp, next
// to the REST API rather than under it: an MCP client is pointed at one URL,
// not at a versioned API path. The tools it serves loop back through this same
// engine in process, so they answer to the handlers, auth and mutation gates
// the REST API already enforces.
func registerMCPEndpoint(r *gin.Engine, h *Handler) {
	if !h.Config.MCP.Enabled {
		return
	}
	server := mcp.NewLocalServer(r, h.Config.MCP.APIToken)
	endpoint := r.Group("/mcp")
	endpoint.Use(middleware.Auth(h.Config))
	endpoint.POST("", h.MCPEndpoint(server))
	endpoint.GET("", MCPMethodNotAllowed)
	endpoint.DELETE("", MCPMethodNotAllowed)
}

func parseUintParam(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		util.Error(c, http.StatusBadRequest, "INVALID_ID", "Invalid id", gin.H{"param": name})
		return 0, false
	}
	return id, true
}
