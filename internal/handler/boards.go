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

type issueResponse struct {
	model.Issue
	Identifier   string `json:"identifier"`
	BoardKey     string `json:"board_key"`
	AssigneeName string `json:"assignee_name,omitempty"`
	CreatorName  string `json:"creator_name,omitempty"`
	// CommentCount lets a card show that a conversation is happening on it
	// without the board fetching every thread.
	CommentCount int `json:"comment_count"`
}

// ListBoards returns the boards still in use. Archived ones are withheld unless
// asked for by name -- the index is a list of where work is happening, and a
// board is archived precisely to stop it answering that question.
func (h *Handler) ListBoards(c *gin.Context) {
	query := h.DB.Where("archived_at IS NULL")
	if archived := c.Query("archived"); archived == "true" || archived == "1" {
		query = h.DB.Where("archived_at IS NOT NULL")
	}
	var boards []model.Board
	if err := query.Order("id asc").Find(&boards).Error; err != nil {
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
		Archived     *bool   `json:"archived"`
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
	// Archiving is idempotent on purpose: re-archiving an archived board keeps
	// the date it was first retired rather than moving it to today.
	if payload.Archived != nil {
		switch {
		case !*payload.Archived:
			updates["archived_at"] = nil
		case board.ArchivedAt == nil:
			now := time.Now()
			updates["archived_at"] = &now
		}
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
	util.OK(c, h.issueResponses(c, board, issues))
}

func (h *Handler) CreateBoardIssue(c *gin.Context) {
	board, ok := h.loadBoard(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
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
	util.Created(c, h.issueResponse(c, board, issue))
}

func (h *Handler) GetIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	util.OK(c, h.issueResponse(c, board, issue))
}

func (h *Handler) UpdateIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
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
	util.OK(c, h.issueResponse(c, board, issue))
}

// MoveIssue places a dragged card. The client sends the neighbours it dropped
// between rather than an index, so a board that has changed underneath it still
// resolves the drop to the slot the user aimed at.
func (h *Handler) MoveIssue(c *gin.Context) {
	issue, board, ok := h.loadIssue(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
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
	util.OK(c, h.issueResponse(c, board, *moved))
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

func (h *Handler) issueResponse(c *gin.Context, board model.Board, issue model.Issue) issueResponse {
	return h.issueResponses(c, board, []model.Issue{issue})[0]
}

// issueResponses resolves the assignee and creator names for a whole column at
// once: a board renders every card, so per-card lookups would be one query per
// card.
func (h *Handler) issueResponses(c *gin.Context, board model.Board, issues []model.Issue) []issueResponse {
	out := make([]issueResponse, 0, len(issues))
	if len(issues) == 0 {
		return out
	}
	userIDs := map[uint64]bool{}
	for _, issue := range issues {
		if issue.AssigneeID != nil {
			userIDs[*issue.AssigneeID] = true
		}
		if issue.CreatorID != nil {
			userIDs[*issue.CreatorID] = true
		}
	}
	names := h.usernames(userIDs)
	issueIDs := make([]uint64, 0, len(issues))
	for _, issue := range issues {
		issueIDs = append(issueIDs, issue.ID)
	}
	commentCounts := h.Service.CountIssueComments(c.Request.Context(), issueIDs)
	for _, issue := range issues {
		if issue.Labels == nil {
			issue.Labels = []string{}
		}
		response := issueResponse{
			Issue:        issue,
			Identifier:   issue.Identifier(board.Key),
			BoardKey:     board.Key,
			CommentCount: commentCounts[issue.ID],
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
