package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/rank"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrBoardNotFound   = errors.New("board not found")
	ErrIssueNotFound   = errors.New("issue not found")
	ErrBoardHasIssues  = errors.New("board still has issues")
	ErrInvalidStatus   = errors.New("unknown issue status")
	ErrInvalidPriority = errors.New("unknown issue priority")
	ErrIssueTitleEmpty = errors.New("issue title is required")
)

// IssueMove describes where a dragged card landed: the column it was dropped in
// and the two cards it came to rest between. Neighbours are named by issue id
// rather than by index because the client's view of the column may already be
// one move out of date.
type IssueMove struct {
	Status   string
	AfterID  uint64
	BeforeID uint64
}

// CreateIssue allocates the board-local number and the initial ordering key,
// then inserts the card at the top of its column.
func (s *Service) CreateIssue(ctx context.Context, boardID uint64, issue *model.Issue) error {
	issue.Title = strings.TrimSpace(issue.Title)
	if issue.Title == "" {
		return ErrIssueTitleEmpty
	}
	if issue.Status == "" {
		issue.Status = model.IssueBacklog
	}
	if !model.IsIssueStatus(issue.Status) {
		return ErrInvalidStatus
	}
	if issue.Priority == "" {
		issue.Priority = model.IssuePriorityNone
	}
	if !model.IsIssuePriority(issue.Priority) {
		return ErrInvalidPriority
	}
	if model.IssueClosed(issue.Status) && issue.CompletedAt == nil {
		now := time.Now()
		issue.CompletedAt = &now
	}
	if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The board row is locked so two issues created at the same instant get
		// two different numbers; the unique index on (board_id, number) is the
		// backstop for databases where the lock is a no-op.
		var board model.Board
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&board, boardID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBoardNotFound
			}
			return err
		}
		board.IssueSeq++
		if err := tx.Model(&board).Update("issue_seq", board.IssueSeq).Error; err != nil {
			return err
		}
		issue.BoardID = board.ID
		issue.Number = board.IssueSeq
		issue.Position = topPosition(tx, board.ID, issue.Status)
		if issue.Labels == nil {
			issue.Labels = []string{}
		}
		return tx.Create(issue).Error
	}); err != nil {
		return err
	}
	s.BindAttachments(ctx, issue.ID, issue.Description)
	s.publishBoard(issue.BoardID, "issue.created", issue.ID)
	return nil
}

// MoveIssue puts an issue in a column between two neighbours. Only the moved
// row is written: the ordering key is computed to fall between the neighbours
// that are already stored, so a concurrent move elsewhere in the column is not
// overwritten.
func (s *Service) MoveIssue(ctx context.Context, issueID uint64, move IssueMove) (*model.Issue, error) {
	if !model.IsIssueStatus(move.Status) {
		return nil, ErrInvalidStatus
	}
	var issue model.Issue
	if err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&issue, issueID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrIssueNotFound
			}
			return err
		}
		position, err := s.positionBetween(tx, issue.BoardID, move, issueID)
		if err != nil {
			return err
		}
		updates := map[string]interface{}{"status": move.Status, "position": position}
		// completed_at tracks when the work actually finished, so it is set when
		// a card enters a closed column and cleared when it is dragged back out.
		switch {
		case model.IssueClosed(move.Status) && !model.IssueClosed(issue.Status):
			updates["completed_at"] = time.Now()
		case !model.IssueClosed(move.Status) && model.IssueClosed(issue.Status):
			updates["completed_at"] = nil
		}
		if err := tx.Model(&issue).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&issue, issueID).Error
	}); err != nil {
		return nil, err
	}
	s.publishBoard(issue.BoardID, "issue.moved", issue.ID)
	return &issue, nil
}

// positionBetween resolves the neighbour ids to ordering keys and returns a key
// between them, rebalancing the column when the stored keys cannot be split.
func (s *Service) positionBetween(tx *gorm.DB, boardID uint64, move IssueMove, movingID uint64) (string, error) {
	if position, ok := s.tryPositionBetween(tx, boardID, move, movingID); ok {
		return position, nil
	}
	// The column's keys cannot express the drop: rows written before ordering
	// keys existed, or a gap split past its limit. Rewrite the column evenly and
	// try once more -- the client's neighbours still name the right slot, so the
	// card lands where it was dropped.
	if err := s.rebalanceColumn(tx, boardID, move.Status, movingID); err != nil {
		return "", err
	}
	if position, ok := s.tryPositionBetween(tx, boardID, move, movingID); ok {
		return position, nil
	}
	return bottomPosition(tx, boardID, move.Status, movingID), nil
}

func (s *Service) tryPositionBetween(tx *gorm.DB, boardID uint64, move IssueMove, movingID uint64) (string, bool) {
	after, afterOK := s.neighbourPosition(tx, boardID, move.Status, move.AfterID, movingID)
	before, beforeOK := s.neighbourPosition(tx, boardID, move.Status, move.BeforeID, movingID)
	if !afterOK || !beforeOK {
		return "", false
	}
	position, err := rank.Between(after, before)
	if err != nil {
		return "", false
	}
	return position, true
}

// neighbourPosition reads a neighbour's ordering key. It reports ok=false when
// the neighbour is really there but its stored key is unusable, which is what
// separates "this card sits at the open end of the column" from "this column's
// keys need rebuilding" -- an empty key would otherwise read as the former and
// silently leave the broken rows in place.
func (s *Service) neighbourPosition(tx *gorm.DB, boardID uint64, status string, neighbourID uint64, movingID uint64) (string, bool) {
	if neighbourID == 0 || neighbourID == movingID {
		return "", true
	}
	var neighbour model.Issue
	if err := tx.Select("id", "position", "status", "board_id").First(&neighbour, neighbourID).Error; err != nil {
		return "", true
	}
	if neighbour.BoardID != boardID || neighbour.Status != status {
		return "", true
	}
	if neighbour.Position == "" || !rank.Valid(neighbour.Position) {
		return "", false
	}
	return neighbour.Position, true
}

func (s *Service) rebalanceColumn(tx *gorm.DB, boardID uint64, status string, excludeID uint64) error {
	var issues []model.Issue
	query := tx.Select("id", "position").Where("board_id = ? AND status = ?", boardID, status)
	if excludeID != 0 {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Order("position asc, id asc").Find(&issues).Error; err != nil {
		return err
	}
	keys := rank.Rebalance(len(issues))
	for i, issue := range issues {
		if err := tx.Model(&model.Issue{}).Where("id = ?", issue.ID).Update("position", keys[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func topPosition(tx *gorm.DB, boardID uint64, status string) string {
	var first model.Issue
	err := tx.Select("id", "position").Where("board_id = ? AND status = ?", boardID, status).Order("position asc").First(&first).Error
	if err != nil {
		return rank.First()
	}
	position, err := rank.Between("", first.Position)
	if err != nil {
		return rank.First()
	}
	return position
}

func bottomPosition(tx *gorm.DB, boardID uint64, status string, excludeID uint64) string {
	var last model.Issue
	query := tx.Select("id", "position").Where("board_id = ? AND status = ?", boardID, status)
	if excludeID != 0 {
		query = query.Where("id <> ?", excludeID)
	}
	if err := query.Order("position desc").First(&last).Error; err != nil {
		return rank.First()
	}
	position, err := rank.Between(last.Position, "")
	if err != nil {
		return rank.First()
	}
	return position
}

// DeleteBoard removes an empty board. A board with issues is refused rather
// than cascaded: deleting a board is not the way anyone means to delete work.
func (s *Service) DeleteBoard(ctx context.Context, boardID uint64) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var board model.Board
		if err := tx.First(&board, boardID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBoardNotFound
			}
			return err
		}
		var count int64
		if err := tx.Model(&model.Issue{}).Where("board_id = ?", boardID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrBoardHasIssues
		}
		return tx.Delete(&board).Error
	})
}

// DeleteIssue removes an issue with the thread and attachments hanging off it.
func (s *Service) DeleteIssue(ctx context.Context, issueID uint64) error {
	var issue model.Issue
	if err := s.DB.WithContext(ctx).First(&issue, issueID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrIssueNotFound
		}
		return err
	}
	if err := s.DB.WithContext(ctx).Delete(&issue).Error; err != nil {
		return err
	}
	s.deleteCommentsForIssue(ctx, issue.ID)
	s.DeleteAttachmentsForIssue(ctx, issue.ID)
	s.publishBoard(issue.BoardID, "issue.deleted", issue.ID)
	return nil
}

// publishBoard notifies open board views that something changed. The payload
// names the change rather than carrying the row: every client refetches the
// board, which is what keeps a viewer from having to merge a partial update
// into a card they are in the middle of dragging.
func (s *Service) publishBoard(boardID uint64, event string, subjectID uint64) {
	if s.Hub == nil || boardID == 0 {
		return
	}
	payload, err := json.Marshal(map[string]interface{}{"event": event, "board_id": boardID, "subject_id": subjectID})
	if err != nil {
		return
	}
	s.Hub.Publish(sse.BoardTopic(boardID), string(payload))
}

// PublishIssueChanged lets a handler that wrote an issue directly -- editing a
// field, attaching a release -- notify the board's open views the same way the
// service's own writes do.
func (s *Service) PublishIssueChanged(boardID uint64, issueID uint64) {
	s.publishBoard(boardID, "issue.updated", issueID)
}
