package handler

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"github.com/hellodeveye/postdare-go/internal/util"
)

type boardResponse struct {
	model.Board
	ProjectName string         `json:"project_name,omitempty"`
	IssueCounts map[string]int `json:"issue_counts"`
}

type issueDeployLinkResponse struct {
	TaskID      uint64     `json:"task_id"`
	ProjectID   uint64     `json:"project_id"`
	ProjectName string     `json:"project_name,omitempty"`
	Status      string     `json:"status"`
	Branch      string     `json:"branch"`
	CommitID    string     `json:"commit_id"`
	Closing     bool       `json:"closing"`
	Source      string     `json:"source"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type issueResponse struct {
	model.Issue
	Identifier   string                    `json:"identifier"`
	BoardKey     string                    `json:"board_key"`
	AssigneeName string                    `json:"assignee_name,omitempty"`
	CreatorName  string                    `json:"creator_name,omitempty"`
	DeployLinks  []issueDeployLinkResponse `json:"deploy_links"`
}

func (h *Handler) ListBoards(c *gin.Context) {
	var boards []model.Board
	if err := h.DB.Order("id asc").Find(&boards).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "BOARD_LIST_FAILED", "Failed to list boards", nil)
		return
	}
	out := make([]boardResponse, 0, len(boards))
	for _, board := range boards {
		out = append(out, h.boardResponse(board))
	}
	util.OK(c, out)
}

func (h *Handler) CreateBoard(c *gin.Context) {
	var payload struct {
		Name        string  `json:"name"`
		Key         string  `json:"key"`
		Description string  `json:"description"`
		ProjectID   *uint64 `json:"project_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid board payload", nil)
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		util.Error(c, http.StatusUnprocessableEntity, "BOARD_NAME_REQUIRED", "Board name is required", nil)
		return
	}
	key := model.NormalizeBoardKey(payload.Key)
	if key == "" {
		util.Error(c, http.StatusUnprocessableEntity, "BOARD_KEY_INVALID", "Board key must be 2-10 characters, starting with a letter", nil)
		return
	}
	if !h.boardProjectExists(c, payload.ProjectID) {
		return
	}
	board := model.Board{Name: name, Key: key, Description: strings.TrimSpace(payload.Description), ProjectID: payload.ProjectID}
	if err := h.DB.Create(&board).Error; err != nil {
		util.Error(c, http.StatusConflict, "BOARD_KEY_TAKEN", "Board key is already in use", nil)
		return
	}
	util.Created(c, h.boardResponse(board))
}

func (h *Handler) GetBoard(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	util.OK(c, h.boardResponse(board))
}

func (h *Handler) UpdateBoard(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	var payload struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		ProjectID    *uint64 `json:"project_id"`
		ClearProject bool    `json:"clear_project"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid board payload", nil)
		return
	}
	updates := map[string]interface{}{}
	if payload.Name != nil {
		name := strings.TrimSpace(*payload.Name)
		if name == "" {
			util.Error(c, http.StatusUnprocessableEntity, "BOARD_NAME_REQUIRED", "Board name is required", nil)
			return
		}
		updates["name"] = name
	}
	if payload.Description != nil {
		updates["description"] = strings.TrimSpace(*payload.Description)
	}
	// The board key is fixed after creation: it is baked into every issue
	// identifier already written into commit messages and chat threads.
	switch {
	case payload.ClearProject:
		updates["project_id"] = nil
	case payload.ProjectID != nil:
		if !h.boardProjectExists(c, payload.ProjectID) {
			return
		}
		updates["project_id"] = *payload.ProjectID
	}
	if len(updates) > 0 {
		if err := h.DB.Model(&board).Updates(updates).Error; err != nil {
			util.Error(c, http.StatusInternalServerError, "BOARD_UPDATE_FAILED", "Failed to update board", nil)
			return
		}
	}
	if err := h.DB.First(&board, board.ID).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "BOARD_UPDATE_FAILED", "Failed to update board", nil)
		return
	}
	util.OK(c, h.boardResponse(board))
}

func (h *Handler) DeleteBoard(c *gin.Context) {
	id, ok := parseUintParam(c, "board_id")
	if !ok {
		return
	}
	if err := h.Service.DeleteBoard(c.Request.Context(), id); err != nil {
		switch {
		case errors.Is(err, service.ErrBoardNotFound):
			util.Error(c, http.StatusNotFound, "BOARD_NOT_FOUND", "Board not found", nil)
		case errors.Is(err, service.ErrBoardHasIssues):
			util.Error(c, http.StatusConflict, "BOARD_HAS_ISSUES", "Delete or move the board's issues first", nil)
		default:
			util.Error(c, http.StatusInternalServerError, "BOARD_DELETE_FAILED", "Failed to delete board", nil)
		}
		return
	}
	util.NoContent(c)
}

// ListBoardIssues returns every issue on the board, ordered the way the columns
// render. A board is loaded whole rather than paged: a kanban view has to place
// every card to be correct, and a page of cards is not a board.
func (h *Handler) ListBoardIssues(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	query := h.DB.Where("board_id = ?", board.ID)
	if status := c.Query("status"); status != "" {
		if !model.IsIssueStatus(status) {
			util.Error(c, http.StatusUnprocessableEntity, "ISSUE_STATUS_INVALID", "Unknown issue status", gin.H{"allowed": model.IssueStatuses()})
			return
		}
		query = query.Where("status = ?", status)
	}
	if assignee := c.Query("assignee_id"); assignee != "" {
		if assignee == "none" {
			query = query.Where("assignee_id IS NULL")
		} else {
			query = query.Where("assignee_id = ?", assignee)
		}
	}
	if priority := c.Query("priority"); priority != "" {
		query = query.Where("priority = ?", priority)
	}
	if search := strings.TrimSpace(c.Query("q")); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(title) LIKE ? OR LOWER(description) LIKE ?", like, like)
	}
	var issues []model.Issue
	if err := query.Order("position asc, id asc").Find(&issues).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "ISSUE_LIST_FAILED", "Failed to list issues", nil)
		return
	}
	util.OK(c, h.issueResponses(board, issues))
}

func (h *Handler) CreateBoardIssue(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	var payload struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Priority    string   `json:"priority"`
		AssigneeID  *uint64  `json:"assignee_id"`
		Labels      []string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid issue payload", nil)
		return
	}
	if !h.issueAssigneeExists(c, payload.AssigneeID) {
		return
	}
	issue := model.Issue{
		Title:       payload.Title,
		Description: strings.TrimSpace(payload.Description),
		Status:      strings.TrimSpace(payload.Status),
		Priority:    strings.TrimSpace(payload.Priority),
		AssigneeID:  payload.AssigneeID,
		Labels:      normalizeLabels(payload.Labels),
		CreatorID:   currentUserID(c),
	}
	if err := h.Service.CreateIssue(c.Request.Context(), board.ID, &issue); err != nil {
		h.issueError(c, err)
		return
	}
	util.Created(c, h.issueResponse(board, issue))
}

func (h *Handler) GetIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	util.OK(c, h.issueResponse(board, issue))
}

func (h *Handler) UpdateIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	var payload struct {
		Title         *string   `json:"title"`
		Description   *string   `json:"description"`
		Status        *string   `json:"status"`
		Priority      *string   `json:"priority"`
		AssigneeID    *uint64   `json:"assignee_id"`
		ClearAssignee bool      `json:"clear_assignee"`
		Labels        *[]string `json:"labels"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid issue payload", nil)
		return
	}
	// The changed fields are carried on a model value with the touched columns
	// named in Select, rather than in a map. A map update bypasses the field's
	// serializer, so Labels -- a []string stored as JSON -- would be handed to
	// the driver as a bare slice and rejected as a row value.
	var changes model.Issue
	changed := make([]string, 0, 5)
	if payload.Title != nil {
		title := strings.TrimSpace(*payload.Title)
		if title == "" {
			util.Error(c, http.StatusUnprocessableEntity, "ISSUE_TITLE_REQUIRED", "Issue title is required", nil)
			return
		}
		changes.Title = title
		changed = append(changed, "title")
	}
	if payload.Description != nil {
		changes.Description = strings.TrimSpace(*payload.Description)
		changed = append(changed, "description")
	}
	if payload.Priority != nil {
		priority := strings.TrimSpace(*payload.Priority)
		if !model.IsIssuePriority(priority) {
			util.Error(c, http.StatusUnprocessableEntity, "ISSUE_PRIORITY_INVALID", "Unknown issue priority", gin.H{"allowed": model.IssuePriorities()})
			return
		}
		changes.Priority = priority
		changed = append(changed, "priority")
	}
	if payload.Labels != nil {
		changes.Labels = normalizeLabels(*payload.Labels)
		changed = append(changed, "labels")
	}
	switch {
	case payload.ClearAssignee:
		changes.AssigneeID = nil
		changed = append(changed, "assignee_id")
	case payload.AssigneeID != nil:
		if !h.issueAssigneeExists(c, payload.AssigneeID) {
			return
		}
		assigneeID := *payload.AssigneeID
		changes.AssigneeID = &assigneeID
		changed = append(changed, "assignee_id")
	}
	// A status change routes through the move path so the card gets a valid
	// ordering key in its new column instead of keeping the old column's.
	if payload.Status != nil && *payload.Status != issue.Status {
		moved, err := h.Service.MoveIssue(c.Request.Context(), issue.ID, service.IssueMove{Status: strings.TrimSpace(*payload.Status)})
		if err != nil {
			h.issueError(c, err)
			return
		}
		issue = *moved
	}
	if len(changed) > 0 {
		// Select names every touched column so a deliberate zero -- an emptied
		// description, a cleared assignee, the last label removed -- is written
		// instead of being skipped as an unset field.
		if err := h.DB.Model(&model.Issue{}).Where("id = ?", issue.ID).Select(changed).Updates(&changes).Error; err != nil {
			util.Error(c, http.StatusInternalServerError, "ISSUE_UPDATE_FAILED", "Failed to update issue", nil)
			return
		}
	}
	if err := h.DB.First(&issue, issue.ID).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "ISSUE_UPDATE_FAILED", "Failed to update issue", nil)
		return
	}
	if payload.Description != nil {
		h.Service.BindAttachments(c.Request.Context(), issue.ID, issue.Description)
	}
	h.Service.PublishIssueChanged(board.ID, issue.ID)
	util.OK(c, h.issueResponse(board, issue))
}

// MoveIssue places a dragged card. The client sends the neighbours it dropped
// between rather than an index, so a board that has changed underneath it still
// resolves the drop to the slot the user aimed at.
func (h *Handler) MoveIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	var payload struct {
		Status   string `json:"status"`
		AfterID  uint64 `json:"after_id"`
		BeforeID uint64 `json:"before_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid move payload", nil)
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = issue.Status
	}
	moved, err := h.Service.MoveIssue(c.Request.Context(), issue.ID, service.IssueMove{
		Status:   status,
		AfterID:  payload.AfterID,
		BeforeID: payload.BeforeID,
	})
	if err != nil {
		h.issueError(c, err)
		return
	}
	util.OK(c, h.issueResponse(board, *moved))
}

func (h *Handler) DeleteIssue(c *gin.Context) {
	id, ok := parseUintParam(c, "issue_id")
	if !ok {
		return
	}
	if err := h.Service.DeleteIssue(c.Request.Context(), id); err != nil {
		h.issueError(c, err)
		return
	}
	util.NoContent(c)
}

// LinkIssueDeployTask attaches a release to an issue by hand, for the case the
// commit message did not name it. A manual link never closes the issue on its
// own: if a person is attaching it after the fact, they can also move the card.
func (h *Handler) LinkIssueDeployTask(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	var payload struct {
		TaskID uint64 `json:"task_id"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.TaskID == 0 {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "task_id is required", nil)
		return
	}
	var task model.DeployTask
	if err := h.DB.First(&task, payload.TaskID).Error; err != nil {
		util.Error(c, http.StatusNotFound, "DEPLOY_TASK_NOT_FOUND", "Deploy task not found", nil)
		return
	}
	link := model.IssueDeployLink{IssueID: issue.ID, TaskID: task.ID, CommitID: task.CommitID, Source: model.IssueLinkManual}
	if err := h.DB.Where("issue_id = ? AND task_id = ?", issue.ID, task.ID).FirstOrCreate(&link).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "ISSUE_LINK_FAILED", "Failed to link deploy task", nil)
		return
	}
	h.Service.PublishIssueChanged(board.ID, issue.ID)
	util.Created(c, h.issueResponse(board, issue))
}

func (h *Handler) UnlinkIssueDeployTask(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	taskID, ok := parseUintParam(c, "task_id")
	if !ok {
		return
	}
	if err := h.DB.Where("issue_id = ? AND task_id = ?", issue.ID, taskID).Delete(&model.IssueDeployLink{}).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "ISSUE_UNLINK_FAILED", "Failed to unlink deploy task", nil)
		return
	}
	h.Service.PublishIssueChanged(board.ID, issue.ID)
	util.NoContent(c)
}

// StreamBoard pushes a line whenever an issue on the board changes, so a second
// person's drag shows up without a refresh. Like the deploy log stream it
// accepts ?access_token= because EventSource cannot set headers.
func (h *Handler) StreamBoard(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	ch, unsubscribe := h.Hub.Subscribe(sse.BoardTopic(board.ID))
	defer unsubscribe()
	c.Stream(func(w io.Writer) bool {
		select {
		case <-c.Request.Context().Done():
			return false
		case line := <-ch:
			c.SSEvent("message", line)
			return true
		}
	})
}

// ListBoardUsers serves the assignee picker. Only the fields a picker needs are
// selected, so the password hash never leaves the database.
func (h *Handler) ListBoardUsers(c *gin.Context) {
	var users []model.User
	if err := h.DB.Select("id", "username", "role").Order("id asc").Find(&users).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "USER_LIST_FAILED", "Failed to list users", nil)
		return
	}
	out := make([]gin.H, 0, len(users))
	for _, user := range users {
		out = append(out, gin.H{"id": user.ID, "username": user.Username, "role": user.Role})
	}
	util.OK(c, out)
}

// ListBoardLabels serves the board's label vocabulary: the distinct labels its
// issues already use. Labels are free text rather than a configured set, so the
// only way to offer a choice instead of a retype is to read back what the board
// has been using.
func (h *Handler) ListBoardLabels(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	var issues []model.Issue
	if err := h.DB.Select("labels").Where("board_id = ?", board.ID).Find(&issues).Error; err != nil {
		util.Error(c, http.StatusInternalServerError, "LABEL_LIST_FAILED", "Failed to list labels", nil)
		return
	}
	// The column is a JSON array, so the distinct set is built here rather than
	// asked of the database, which cannot index into it portably.
	seen := map[string]string{}
	for _, issue := range issues {
		for _, label := range issue.Labels {
			key := strings.ToLower(strings.TrimSpace(label))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = strings.TrimSpace(label)
			}
		}
	}
	labels := make([]string, 0, len(seen))
	for _, label := range seen {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(i, j int) bool { return strings.ToLower(labels[i]) < strings.ToLower(labels[j]) })
	util.OK(c, labels)
}

// IssueMetadata tells the client which statuses and priorities the server will
// accept, so the board's columns and pickers cannot drift from the model.
func (h *Handler) IssueMetadata(c *gin.Context) {
	util.OK(c, gin.H{"statuses": model.IssueStatuses(), "priorities": model.IssuePriorities()})
}

func (h *Handler) loadBoard(c *gin.Context) (model.Board, bool) {
	id, ok := parseUintParam(c, "board_id")
	if !ok {
		return model.Board{}, false
	}
	var board model.Board
	if err := h.DB.First(&board, id).Error; err != nil {
		util.Error(c, http.StatusNotFound, "BOARD_NOT_FOUND", "Board not found", nil)
		return model.Board{}, false
	}
	return board, true
}

func (h *Handler) loadIssue(c *gin.Context) (model.Issue, model.Board, bool) {
	id, ok := parseUintParam(c, "issue_id")
	if !ok {
		return model.Issue{}, model.Board{}, false
	}
	var issue model.Issue
	if err := h.DB.First(&issue, id).Error; err != nil {
		util.Error(c, http.StatusNotFound, "ISSUE_NOT_FOUND", "Issue not found", nil)
		return model.Issue{}, model.Board{}, false
	}
	var board model.Board
	if err := h.DB.First(&board, issue.BoardID).Error; err != nil {
		util.Error(c, http.StatusNotFound, "BOARD_NOT_FOUND", "Board not found", nil)
		return model.Issue{}, model.Board{}, false
	}
	return issue, board, true
}

func (h *Handler) boardProjectExists(c *gin.Context, projectID *uint64) bool {
	if projectID == nil || *projectID == 0 {
		return true
	}
	var count int64
	if err := h.DB.Model(&model.Project{}).Where("id = ?", *projectID).Count(&count).Error; err != nil || count == 0 {
		util.Error(c, http.StatusUnprocessableEntity, "PROJECT_NOT_FOUND", "Linked project not found", nil)
		return false
	}
	return true
}

func (h *Handler) issueAssigneeExists(c *gin.Context, assigneeID *uint64) bool {
	if assigneeID == nil || *assigneeID == 0 {
		return true
	}
	var count int64
	if err := h.DB.Model(&model.User{}).Where("id = ?", *assigneeID).Count(&count).Error; err != nil || count == 0 {
		util.Error(c, http.StatusUnprocessableEntity, "ASSIGNEE_NOT_FOUND", "Assignee not found", nil)
		return false
	}
	return true
}

func (h *Handler) issueError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrIssueNotFound):
		util.Error(c, http.StatusNotFound, "ISSUE_NOT_FOUND", "Issue not found", nil)
	case errors.Is(err, service.ErrBoardNotFound):
		util.Error(c, http.StatusNotFound, "BOARD_NOT_FOUND", "Board not found", nil)
	case errors.Is(err, service.ErrIssueTitleEmpty):
		util.Error(c, http.StatusUnprocessableEntity, "ISSUE_TITLE_REQUIRED", "Issue title is required", nil)
	case errors.Is(err, service.ErrInvalidStatus):
		util.Error(c, http.StatusUnprocessableEntity, "ISSUE_STATUS_INVALID", "Unknown issue status", gin.H{"allowed": model.IssueStatuses()})
	case errors.Is(err, service.ErrInvalidPriority):
		util.Error(c, http.StatusUnprocessableEntity, "ISSUE_PRIORITY_INVALID", "Unknown issue priority", gin.H{"allowed": model.IssuePriorities()})
	default:
		util.Error(c, http.StatusInternalServerError, "ISSUE_WRITE_FAILED", "Failed to write issue", nil)
	}
}

func (h *Handler) boardResponse(board model.Board) boardResponse {
	response := boardResponse{Board: board, IssueCounts: map[string]int{}}
	for _, status := range model.IssueStatuses() {
		response.IssueCounts[status] = 0
	}
	var rows []struct {
		Status string
		Count  int
	}
	if err := h.DB.Model(&model.Issue{}).Select("status, COUNT(*) as count").Where("board_id = ?", board.ID).Group("status").Scan(&rows).Error; err == nil {
		for _, row := range rows {
			response.IssueCounts[row.Status] = row.Count
		}
	}
	if board.ProjectID != nil {
		var project model.Project
		if err := h.DB.Select("id", "name").First(&project, *board.ProjectID).Error; err == nil {
			response.ProjectName = project.Name
		}
	}
	return response
}

func (h *Handler) issueResponse(board model.Board, issue model.Issue) issueResponse {
	return h.issueResponses(board, []model.Issue{issue})[0]
}

// issueResponses resolves the names and deploy links for a whole column at once:
// a board renders every card, so per-card lookups would be one query per card.
func (h *Handler) issueResponses(board model.Board, issues []model.Issue) []issueResponse {
	out := make([]issueResponse, 0, len(issues))
	if len(issues) == 0 {
		return out
	}
	issueIDs := make([]uint64, 0, len(issues))
	userIDs := map[uint64]bool{}
	for _, issue := range issues {
		issueIDs = append(issueIDs, issue.ID)
		if issue.AssigneeID != nil {
			userIDs[*issue.AssigneeID] = true
		}
		if issue.CreatorID != nil {
			userIDs[*issue.CreatorID] = true
		}
	}
	names := h.usernames(userIDs)
	links := h.deployLinks(issueIDs)
	for _, issue := range issues {
		if issue.Labels == nil {
			issue.Labels = []string{}
		}
		response := issueResponse{
			Issue:       issue,
			Identifier:  issue.Identifier(board.Key),
			BoardKey:    board.Key,
			DeployLinks: links[issue.ID],
		}
		if response.DeployLinks == nil {
			response.DeployLinks = []issueDeployLinkResponse{}
		}
		if issue.AssigneeID != nil {
			response.AssigneeName = names[*issue.AssigneeID]
		}
		if issue.CreatorID != nil {
			response.CreatorName = names[*issue.CreatorID]
		}
		out = append(out, response)
	}
	return out
}

func (h *Handler) usernames(ids map[uint64]bool) map[uint64]string {
	names := map[uint64]string{}
	if len(ids) == 0 {
		return names
	}
	list := make([]uint64, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	var users []model.User
	if err := h.DB.Select("id", "username").Where("id IN ?", list).Find(&users).Error; err != nil {
		return names
	}
	for _, user := range users {
		names[user.ID] = user.Username
	}
	return names
}

func (h *Handler) deployLinks(issueIDs []uint64) map[uint64][]issueDeployLinkResponse {
	grouped := map[uint64][]issueDeployLinkResponse{}
	var links []model.IssueDeployLink
	if err := h.DB.Where("issue_id IN ?", issueIDs).Order("task_id desc").Find(&links).Error; err != nil || len(links) == 0 {
		return grouped
	}
	taskIDs := make([]uint64, 0, len(links))
	for _, link := range links {
		taskIDs = append(taskIDs, link.TaskID)
	}
	var tasks []model.DeployTask
	if err := h.DB.Select("id", "project_id", "status", "branch", "commit_id", "finished_at").Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
		return grouped
	}
	tasksByID := map[uint64]model.DeployTask{}
	projectIDs := map[uint64]bool{}
	for _, task := range tasks {
		tasksByID[task.ID] = task
		projectIDs[task.ProjectID] = true
	}
	projectNames := map[uint64]string{}
	if len(projectIDs) > 0 {
		list := make([]uint64, 0, len(projectIDs))
		for id := range projectIDs {
			list = append(list, id)
		}
		var projects []model.Project
		if err := h.DB.Select("id", "name").Where("id IN ?", list).Find(&projects).Error; err == nil {
			for _, project := range projects {
				projectNames[project.ID] = project.Name
			}
		}
	}
	for _, link := range links {
		task, ok := tasksByID[link.TaskID]
		if !ok {
			continue
		}
		grouped[link.IssueID] = append(grouped[link.IssueID], issueDeployLinkResponse{
			TaskID:      link.TaskID,
			ProjectID:   task.ProjectID,
			ProjectName: projectNames[task.ProjectID],
			Status:      task.Status,
			Branch:      task.Branch,
			CommitID:    link.CommitID,
			Closing:     link.Closing,
			Source:      link.Source,
			FinishedAt:  task.FinishedAt,
			CreatedAt:   link.CreatedAt,
		})
	}
	return grouped
}

// normalizeLabels trims, de-duplicates and caps the label list so a card cannot
// carry blank chips or an unbounded JSON blob.
func normalizeLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	seen := map[string]bool{}
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || len(label) > 40 || seen[strings.ToLower(label)] {
			continue
		}
		seen[strings.ToLower(label)] = true
		out = append(out, label)
		if len(out) == 10 {
			break
		}
	}
	return out
}

func currentUserID(c *gin.Context) *uint64 {
	value, ok := c.Get(middleware.UserIDKey)
	if !ok {
		return nil
	}
	id, ok := value.(uint64)
	if !ok || id == 0 {
		return nil
	}
	return &id
}
