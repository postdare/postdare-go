package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	IssueBacklog    = "backlog"
	IssueTodo       = "todo"
	IssueInProgress = "in_progress"
	IssueDone       = "done"
	IssueCanceled   = "canceled"

	IssuePriorityNone   = "none"
	IssuePriorityUrgent = "urgent"
	IssuePriorityHigh   = "high"
	IssuePriorityMedium = "medium"
	IssuePriorityLow    = "low"
)

// boardKeyPattern is what makes an identifier like ENG-42 unambiguous in a
// commit message: leading letter, uppercase, short enough to type.
var boardKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// IssueStatuses lists the board columns from intake to closed. The set is fixed
// rather than user-defined so that "did this deploy finish the work" has one
// answer the deploy pipeline can act on without extra configuration.
func IssueStatuses() []string {
	return []string{IssueBacklog, IssueTodo, IssueInProgress, IssueDone, IssueCanceled}
}

// IsIssueStatus reports whether value names a board column.
func IsIssueStatus(value string) bool {
	return contains(IssueStatuses(), value)
}

// IssueClosed reports whether a status means the issue needs no more work.
func IssueClosed(status string) bool {
	return status == IssueDone || status == IssueCanceled
}

// IssuePriorities lists priorities from most to least urgent, with "none" last
// so an unprioritized issue sorts below a deliberately low one.
func IssuePriorities() []string {
	return []string{IssuePriorityUrgent, IssuePriorityHigh, IssuePriorityMedium, IssuePriorityLow, IssuePriorityNone}
}

// IsIssuePriority reports whether value names a priority.
func IsIssuePriority(value string) bool {
	return contains(IssuePriorities(), value)
}

// NormalizeBoardKey uppercases and trims a board key, returning "" when the
// result could not be used as an issue identifier prefix.
func NormalizeBoardKey(value string) string {
	key := strings.ToUpper(strings.TrimSpace(value))
	if !boardKeyPattern.MatchString(key) {
		return ""
	}
	return key
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// Board groups issues and owns their numbering. It is deliberately a separate
// entity from Project: a Project is a deployable service (repo, app dir, deploy
// stages) and a Board is a stream of work, and the two are only sometimes the
// same thing. ProjectID is the optional bridge between them.
type Board struct {
	ID          uint64    `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:100;not null" json:"name"`
	Key         string    `gorm:"size:10;uniqueIndex;not null" json:"key"`
	Description string    `gorm:"type:text" json:"description"`
	ProjectID   *uint64   `gorm:"index:idx_boards_project_id" json:"project_id"`
	IssueSeq    uint64    `gorm:"not null;default:0" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Issue is one card on a board.
type Issue struct {
	ID      uint64 `gorm:"primaryKey" json:"id"`
	BoardID uint64 `gorm:"not null;uniqueIndex:idx_issues_board_number,priority:1;index:idx_issues_board_id" json:"board_id"`
	// Number is the board-local counter behind the ENG-42 identifier. It is
	// allocated under a lock on the board row, so two issues created at once
	// cannot claim the same one.
	Number      uint64   `gorm:"not null;uniqueIndex:idx_issues_board_number,priority:2" json:"number"`
	Title       string   `gorm:"size:300;not null" json:"title"`
	Description string   `gorm:"type:text" json:"description"`
	Status      string   `gorm:"size:50;not null;index:idx_issues_status" json:"status"`
	Priority    string   `gorm:"size:50;not null;default:none" json:"priority"`
	AssigneeID  *uint64  `gorm:"index:idx_issues_assignee_id" json:"assignee_id"`
	Labels      []string `gorm:"serializer:json;type:json" json:"labels"`
	// Position orders the issue inside its column. See internal/rank.
	Position    string     `gorm:"size:100;not null" json:"position"`
	CreatorID   *uint64    `json:"creator_id"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `gorm:"index:idx_issues_created_at" json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Identifier renders the human-facing issue key, e.g. ENG-42. The board key is
// passed in because an issue row does not carry it.
func (i Issue) Identifier(boardKey string) string {
	if boardKey == "" {
		return fmt.Sprintf("#%d", i.Number)
	}
	return fmt.Sprintf("%s-%d", boardKey, i.Number)
}

// AttachmentContentTypes are the image types an issue may carry, keyed by the
// extension their stored file gets.
//
// The list is raster-only on purpose. An SVG served from this origin can run
// script, and the session token lives in the browser's local storage, so
// accepting one would hand anyone who can file an issue a way to take over the
// account of anyone who reads it. Nothing here can execute.
func AttachmentContentTypes() map[string]string {
	return map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
}

// AttachmentExtension returns the stored file extension for a sniffed content
// type, and "" when the type is not one an issue may carry.
func AttachmentExtension(contentType string) string {
	return AttachmentContentTypes()[strings.ToLower(strings.TrimSpace(contentType))]
}

// Attachment is an image uploaded for an issue's description.
//
// IssueID is nil between the upload and the save: an image is pasted while the
// issue is still being written, so the row exists before there is an issue to
// hang it on, and is bound when a description that references it is saved.
type Attachment struct {
	ID         uint64  `gorm:"primaryKey" json:"id"`
	IssueID    *uint64 `gorm:"index:idx_attachments_issue_id" json:"issue_id"`
	UploaderID *uint64 `json:"uploader_id"`
	// Filename is what the uploader called it, kept for the alt text and the
	// download name only. It never reaches the filesystem.
	Filename string `gorm:"size:255" json:"filename"`
	// StoredName is the server-generated name on disk, so a crafted upload name
	// cannot choose where the bytes land or what they are served as.
	StoredName string `gorm:"size:255;not null" json:"-"`
	// ContentType is what the bytes were sniffed to be, never what the client
	// claimed, and it is what the download response declares.
	ContentType string    `gorm:"size:100;not null" json:"content_type"`
	Size        int64     `gorm:"not null" json:"size"`
	CreatedAt   time.Time `gorm:"index:idx_attachments_created_at" json:"created_at"`
}
