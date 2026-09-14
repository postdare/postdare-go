package service

import (
	"context"
	"errors"
	"strings"

	"github.com/hellodeveye/postdare-go/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrCommentNotFound = errors.New("comment not found")
	ErrCommentEmpty    = errors.New("comment body is required")
	ErrCommentTooLong  = errors.New("comment is too long")
)

// ListIssueComments returns an issue's thread oldest first, which is the order
// it was written in and the order it reads in. The thread is returned whole
// rather than paged: it is the conversation about one card, and a page of it
// would hide the reply that the last one answers.
func (s *Service) ListIssueComments(ctx context.Context, issueID uint64) ([]model.IssueComment, error) {
	var comments []model.IssueComment
	if err := s.DB.WithContext(ctx).Where("issue_id = ?", issueID).Order("created_at asc, id asc").Find(&comments).Error; err != nil {
		return nil, err
	}
	return comments, nil
}

// CountIssueComments returns how many comments each of the given issues has, so
// a board can render the count on every card with one query instead of one per
// card. Issues with no comments are absent from the map rather than zero.
func (s *Service) CountIssueComments(ctx context.Context, issueIDs []uint64) map[uint64]int {
	counts := map[uint64]int{}
	if len(issueIDs) == 0 {
		return counts
	}
	var rows []struct {
		IssueID uint64
		Count   int
	}
	if err := s.DB.WithContext(ctx).Model(&model.IssueComment{}).
		Select("issue_id, COUNT(*) as count").
		Where("issue_id IN ?", issueIDs).
		Group("issue_id").Scan(&rows).Error; err != nil {
		s.Logger.Warn("count issue comments failed", zap.Error(err))
		return counts
	}
	for _, row := range rows {
		counts[row.IssueID] = row.Count
	}
	return counts
}

// CreateIssueComment appends a comment to an issue's thread.
//
// The images the body refers to are bound to the issue, not to the comment:
// deleting the issue already takes its attachments with it, and a screenshot
// posted in a thread should not outlive the card it was posted on.
func (s *Service) CreateIssueComment(ctx context.Context, issueID uint64, authorID *uint64, body string) (*model.IssueComment, error) {
	body, err := normalizeCommentBody(body)
	if err != nil {
		return nil, err
	}
	var issue model.Issue
	if err := s.DB.WithContext(ctx).First(&issue, issueID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrIssueNotFound
		}
		return nil, err
	}
	comment := model.IssueComment{IssueID: issue.ID, AuthorID: authorID, Body: body}
	if err := s.DB.WithContext(ctx).Create(&comment).Error; err != nil {
		return nil, err
	}
	s.BindAttachments(ctx, issue.ID, comment.Body)
	s.publishBoard(issue.BoardID, "issue.commented", issue.ID)
	return &comment, nil
}

// UpdateIssueComment rewrites a comment's body in place. Only the body can
// change: who wrote it and when the thread reached it are the parts that make
// the conversation readable later.
func (s *Service) UpdateIssueComment(ctx context.Context, commentID uint64, body string) (*model.IssueComment, error) {
	body, err := normalizeCommentBody(body)
	if err != nil {
		return nil, err
	}
	comment, err := s.LoadIssueComment(ctx, commentID)
	if err != nil {
		return nil, err
	}
	if err := s.DB.WithContext(ctx).Model(comment).Update("body", body).Error; err != nil {
		return nil, err
	}
	s.BindAttachments(ctx, comment.IssueID, body)
	s.publishBoard(s.issueBoardID(ctx, comment.IssueID), "issue.commented", comment.IssueID)
	return comment, nil
}

// DeleteIssueComment removes one comment. The images it referred to stay with
// the issue: another comment, or the description, may quote the same one.
func (s *Service) DeleteIssueComment(ctx context.Context, commentID uint64) error {
	comment, err := s.LoadIssueComment(ctx, commentID)
	if err != nil {
		return err
	}
	if err := s.DB.WithContext(ctx).Delete(comment).Error; err != nil {
		return err
	}
	s.publishBoard(s.issueBoardID(ctx, comment.IssueID), "issue.commented", comment.IssueID)
	return nil
}

// LoadIssueComment reads one comment, translating a missing row into the error
// the handler answers 404 with.
func (s *Service) LoadIssueComment(ctx context.Context, commentID uint64) (*model.IssueComment, error) {
	var comment model.IssueComment
	if err := s.DB.WithContext(ctx).First(&comment, commentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCommentNotFound
		}
		return nil, err
	}
	return &comment, nil
}

// deleteCommentsForIssue clears a deleted issue's thread. A failure is logged
// rather than returned: the issue is gone either way, and a stranded thread is
// unreachable rather than harmful.
func (s *Service) deleteCommentsForIssue(ctx context.Context, issueID uint64) {
	if err := s.DB.WithContext(ctx).Where("issue_id = ?", issueID).Delete(&model.IssueComment{}).Error; err != nil {
		s.Logger.Warn("delete issue comments failed", zap.Uint64("issue_id", issueID), zap.Error(err))
	}
}

func (s *Service) issueBoardID(ctx context.Context, issueID uint64) uint64 {
	var issue model.Issue
	if err := s.DB.WithContext(ctx).Select("id", "board_id").First(&issue, issueID).Error; err != nil {
		return 0
	}
	return issue.BoardID
}

// normalizeCommentBody trims the body and holds it to a size. The trim is what
// makes "posted an empty comment" impossible from a stray newline in the
// editor, which serializes an empty document as one.
func normalizeCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ErrCommentEmpty
	}
	if len([]rune(body)) > model.MaxIssueCommentLength {
		return "", ErrCommentTooLong
	}
	return body, nil
}
