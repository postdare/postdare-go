package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/middleware"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/util"
)

type commentResponse struct {
	model.IssueComment
	AuthorName string `json:"author_name,omitempty"`
	// Edited says the body was rewritten after it was posted, so the reader can
	// tell a remark that has been revised from the one they remember.
	Edited bool `json:"edited"`
}

// ListIssueComments returns the issue's thread.
func (h *Handler) ListIssueComments(c *gin.Context) {
	issue, _, ok := h.loadIssue(c)
	if !ok {
		return
	}
	comments, err := h.Service.ListIssueComments(c.Request.Context(), issue.ID)
	if err != nil {
		util.Error(c, http.StatusInternalServerError, "COMMENT_LIST_FAILED", "Failed to list comments", nil)
		return
	}
	util.OK(c, h.commentResponses(comments))
}

// CreateIssueComment posts one comment. The body is markdown, stored as typed:
// it is rendered, never interpolated into a page, and the reader sanitizes it.
func (h *Handler) CreateIssueComment(c *gin.Context) {
	issue, _, ok := h.loadIssue(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
		return
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid comment payload", nil)
		return
	}
	comment, err := h.Service.CreateIssueComment(c.Request.Context(), issue.ID, currentUserID(c), payload.Body)
	if err != nil {
		h.commentError(c, err)
		return
	}
	util.Created(c, h.commentResponse(*comment))
}

// UpdateIssueComment rewrites a comment the caller is allowed to rewrite.
func (h *Handler) UpdateIssueComment(c *gin.Context) {
	comment, ok := h.loadComment(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
		return
	}
	if !h.mayWriteComment(c, comment) {
		return
	}
	var payload struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		util.Error(c, http.StatusBadRequest, "INVALID_PAYLOAD", "Invalid comment payload", nil)
		return
	}
	updated, err := h.Service.UpdateIssueComment(c.Request.Context(), comment.ID, payload.Body)
	if err != nil {
		h.commentError(c, err)
		return
	}
	util.OK(c, h.commentResponse(*updated))
}

// DeleteIssueComment removes a comment the caller is allowed to remove.
func (h *Handler) DeleteIssueComment(c *gin.Context) {
	comment, ok := h.loadComment(c)
	if !ok {
		return
	}
	if !h.allowMCPMutation(c) {
		return
	}
	if !h.mayWriteComment(c, comment) {
		return
	}
	if err := h.Service.DeleteIssueComment(c.Request.Context(), comment.ID); err != nil {
		h.commentError(c, err)
		return
	}
	util.NoContent(c)
}

func (h *Handler) loadComment(c *gin.Context) (model.IssueComment, bool) {
	id, ok := parseUintParam(c, "comment_id")
	if !ok {
		return model.IssueComment{}, false
	}
	comment, err := h.Service.LoadIssueComment(c.Request.Context(), id)
	if err != nil {
		util.Error(c, http.StatusNotFound, "COMMENT_NOT_FOUND", "Comment not found", nil)
		return model.IssueComment{}, false
	}
	return *comment, true
}

// mayWriteComment answers who can edit or delete one: the person who wrote it,
// and an admin. A remark is attributed, so letting anyone rewrite it would put
// words in someone else's name; an admin keeps a way to remove what should not
// stand on the board.
func (h *Handler) mayWriteComment(c *gin.Context, comment model.IssueComment) bool {
	if strings.EqualFold(c.GetString(middleware.RoleKey), "admin") {
		return true
	}
	userID := currentUserID(c)
	if userID != nil && comment.AuthorID != nil && *userID == *comment.AuthorID {
		return true
	}
	util.Error(c, http.StatusForbidden, "COMMENT_FORBIDDEN", "Only the author can change this comment", nil)
	return false
}

func (h *Handler) commentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrIssueNotFound):
		util.Error(c, http.StatusNotFound, "ISSUE_NOT_FOUND", "Issue not found", nil)
	case errors.Is(err, service.ErrCommentNotFound):
		util.Error(c, http.StatusNotFound, "COMMENT_NOT_FOUND", "Comment not found", nil)
	case errors.Is(err, service.ErrCommentEmpty):
		util.Error(c, http.StatusUnprocessableEntity, "COMMENT_BODY_REQUIRED", "Comment body is required", nil)
	case errors.Is(err, service.ErrCommentTooLong):
		util.Error(c, http.StatusUnprocessableEntity, "COMMENT_TOO_LONG", "Comment is too long", gin.H{"max_length": model.MaxIssueCommentLength})
	default:
		util.Error(c, http.StatusInternalServerError, "COMMENT_WRITE_FAILED", "Failed to write comment", nil)
	}
}

func (h *Handler) commentResponse(comment model.IssueComment) commentResponse {
	return h.commentResponses([]model.IssueComment{comment})[0]
}

// commentResponses resolves the author names for a whole thread at once, the
// same way a board resolves a column's assignees.
func (h *Handler) commentResponses(comments []model.IssueComment) []commentResponse {
	out := make([]commentResponse, 0, len(comments))
	if len(comments) == 0 {
		return out
	}
	userIDs := map[uint64]bool{}
	for _, comment := range comments {
		if comment.AuthorID != nil {
			userIDs[*comment.AuthorID] = true
		}
	}
	names := h.usernames(userIDs)
	for _, comment := range comments {
		response := commentResponse{
			IssueComment: comment,
			// A row written and never touched again still gets an UpdatedAt a
			// hair past its CreatedAt, so "edited" needs a margin rather than a
			// strict comparison.
			Edited: comment.UpdatedAt.Sub(comment.CreatedAt) > time.Second,
		}
		if comment.AuthorID != nil {
			response.AuthorName = names[*comment.AuthorID]
		}
		out = append(out, response)
	}
	return out
}
