package service

import (
	"context"
	"testing"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/webhook"
)

func newBoardService(t *testing.T) *Service {
	t.Helper()
	svc := newTestService(t)
	if err := svc.DB.AutoMigrate(&model.Board{}, &model.Issue{}, &model.IssueDeployLink{}, &model.User{}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func newBoard(t *testing.T, svc *Service, key string) model.Board {
	t.Helper()
	board := model.Board{Name: key + " board", Key: key}
	if err := svc.DB.Create(&board).Error; err != nil {
		t.Fatal(err)
	}
	return board
}

func newIssue(t *testing.T, svc *Service, boardID uint64, title string, status string) model.Issue {
	t.Helper()
	issue := model.Issue{Title: title, Status: status}
	if err := svc.CreateIssue(context.Background(), boardID, &issue); err != nil {
		t.Fatal(err)
	}
	return issue
}

func columnIDs(t *testing.T, svc *Service, boardID uint64, status string) []uint64 {
	t.Helper()
	var issues []model.Issue
	if err := svc.DB.Where("board_id = ? AND status = ?", boardID, status).Order("position asc, id asc").Find(&issues).Error; err != nil {
		t.Fatal(err)
	}
	ids := make([]uint64, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

func TestCreateIssueNumbersPerBoard(t *testing.T) {
	svc := newBoardService(t)
	eng := newBoard(t, svc, "ENG")
	ops := newBoard(t, svc, "OPS")

	first := newIssue(t, svc, eng.ID, "first", "")
	second := newIssue(t, svc, eng.ID, "second", "")
	other := newIssue(t, svc, ops.ID, "other", "")

	if first.Number != 1 || second.Number != 2 {
		t.Fatalf("expected per-board numbering 1,2 got %d,%d", first.Number, second.Number)
	}
	// A second board restarts at 1: the identifier is scoped by its key.
	if other.Number != 1 {
		t.Fatalf("expected the second board to restart at 1, got %d", other.Number)
	}
	if got := second.Identifier("ENG"); got != "ENG-2" {
		t.Fatalf("expected ENG-2, got %s", got)
	}
}

func TestCreateIssueDefaultsAndValidation(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")

	issue := model.Issue{Title: "  spaced  "}
	if err := svc.CreateIssue(context.Background(), board.ID, &issue); err != nil {
		t.Fatal(err)
	}
	if issue.Title != "spaced" || issue.Status != model.IssueBacklog || issue.Priority != model.IssuePriorityNone {
		t.Fatalf("unexpected defaults %#v", issue)
	}

	if err := svc.CreateIssue(context.Background(), board.ID, &model.Issue{Title: "   "}); err != ErrIssueTitleEmpty {
		t.Fatalf("expected ErrIssueTitleEmpty, got %v", err)
	}
	if err := svc.CreateIssue(context.Background(), board.ID, &model.Issue{Title: "x", Status: "nope"}); err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	if err := svc.CreateIssue(context.Background(), board.ID, &model.Issue{Title: "x", Priority: "nope"}); err != ErrInvalidPriority {
		t.Fatalf("expected ErrInvalidPriority, got %v", err)
	}
	if err := svc.CreateIssue(context.Background(), 4040, &model.Issue{Title: "x"}); err != ErrBoardNotFound {
		t.Fatalf("expected ErrBoardNotFound, got %v", err)
	}
}

// New cards land at the top of their column, the way a freshly filed issue does
// in Linear -- the newest work is the work you are looking at.
func TestCreateIssueLandsOnTopOfColumn(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	first := newIssue(t, svc, board.ID, "first", model.IssueTodo)
	second := newIssue(t, svc, board.ID, "second", model.IssueTodo)
	third := newIssue(t, svc, board.ID, "third", model.IssueTodo)

	want := []uint64{third.ID, second.ID, first.ID}
	if got := columnIDs(t, svc, board.ID, model.IssueTodo); !equalIDs(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestMoveIssueWithinColumn(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	c := newIssue(t, svc, board.ID, "c", model.IssueTodo)
	b := newIssue(t, svc, board.ID, "b", model.IssueTodo)
	a := newIssue(t, svc, board.ID, "a", model.IssueTodo)
	// Column reads a, b, c. Drag a to the bottom.
	if _, err := svc.MoveIssue(context.Background(), a.ID, IssueMove{Status: model.IssueTodo, AfterID: c.ID}); err != nil {
		t.Fatal(err)
	}
	want := []uint64{b.ID, c.ID, a.ID}
	if got := columnIDs(t, svc, board.ID, model.IssueTodo); !equalIDs(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	// And back into the middle, between b and c.
	if _, err := svc.MoveIssue(context.Background(), a.ID, IssueMove{Status: model.IssueTodo, AfterID: b.ID, BeforeID: c.ID}); err != nil {
		t.Fatal(err)
	}
	want = []uint64{b.ID, a.ID, c.ID}
	if got := columnIDs(t, svc, board.ID, model.IssueTodo); !equalIDs(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// Only the dragged row may be rewritten: that is what lets two people reorder
// different parts of a column without overwriting each other.
func TestMoveIssueWritesOnlyTheMovedRow(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	c := newIssue(t, svc, board.ID, "c", model.IssueTodo)
	b := newIssue(t, svc, board.ID, "b", model.IssueTodo)
	a := newIssue(t, svc, board.ID, "a", model.IssueTodo)

	before := map[uint64]string{}
	var rows []model.Issue
	if err := svc.DB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		before[row.ID] = row.Position
	}

	if _, err := svc.MoveIssue(context.Background(), a.ID, IssueMove{Status: model.IssueTodo, AfterID: b.ID, BeforeID: c.ID}); err != nil {
		t.Fatal(err)
	}

	if err := svc.DB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		changed := row.Position != before[row.ID]
		if row.ID == a.ID && !changed {
			t.Fatalf("expected the moved issue's position to change")
		}
		if row.ID != a.ID && changed {
			t.Fatalf("issue %d was rewritten by a move that did not touch it", row.ID)
		}
	}
	_ = c
}

func TestMoveIssueAcrossColumnsTracksCompletion(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "ship it", model.IssueInProgress)

	done, err := svc.MoveIssue(context.Background(), issue.ID, IssueMove{Status: model.IssueDone})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != model.IssueDone || done.CompletedAt == nil {
		t.Fatalf("expected a completed issue, got %#v", done)
	}

	// Dragging it back out clears the completion timestamp, so "when did this
	// finish" never reports a time the issue was later reopened from.
	reopened, err := svc.MoveIssue(context.Background(), issue.ID, IssueMove{Status: model.IssueTodo})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != model.IssueTodo || reopened.CompletedAt != nil {
		t.Fatalf("expected completed_at to be cleared, got %#v", reopened)
	}
}

func TestMoveIssueRejectsUnknownStatus(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "x", model.IssueTodo)
	if _, err := svc.MoveIssue(context.Background(), issue.ID, IssueMove{Status: "shipped"}); err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
	if _, err := svc.MoveIssue(context.Background(), 9090, IssueMove{Status: model.IssueTodo}); err != ErrIssueNotFound {
		t.Fatalf("expected ErrIssueNotFound, got %v", err)
	}
}

// A column whose stored keys cannot be split -- rows written before ordering
// keys existed, all sharing a default -- must still accept a drop.
func TestMoveIssueRecoversFromUnusablePositions(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	a := newIssue(t, svc, board.ID, "a", model.IssueTodo)
	b := newIssue(t, svc, board.ID, "b", model.IssueTodo)
	c := newIssue(t, svc, board.ID, "c", model.IssueTodo)
	if err := svc.DB.Model(&model.Issue{}).Where("board_id = ?", board.ID).Update("position", "").Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.MoveIssue(context.Background(), a.ID, IssueMove{Status: model.IssueTodo, AfterID: b.ID, BeforeID: c.ID}); err != nil {
		t.Fatalf("expected the move to recover, got %v", err)
	}
	ids := columnIDs(t, svc, board.ID, model.IssueTodo)
	if len(ids) != 3 {
		t.Fatalf("expected 3 issues in the column, got %v", ids)
	}
	var rows []model.Issue
	if err := svc.DB.Where("board_id = ?", board.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Position == "" {
			t.Fatalf("issue %d still has an empty position", row.ID)
		}
	}
}

func TestLinkIssuesFromEventDrawsLinks(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	fixed := newIssue(t, svc, board.ID, "fixed", model.IssueInProgress)
	mentioned := newIssue(t, svc, board.ID, "mentioned", model.IssueTodo)

	event := &webhook.Event{Commits: []webhook.Commit{
		{ID: "c1", Message: "fixes ENG-" + itoa(fixed.Number)},
		{ID: "c2", Message: "see ENG-" + itoa(mentioned.Number)},
		{ID: "c3", Message: "touches NOPE-1"},
	}}
	svc.LinkIssuesFromEvent(context.Background(), 77, event)

	var links []model.IssueDeployLink
	if err := svc.DB.Where("task_id = ?", 77).Order("issue_id asc").Find(&links).Error; err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %#v", links)
	}
	if !links[0].Closing || links[0].CommitID != "c1" {
		t.Fatalf("expected the closing link to carry its commit, got %#v", links[0])
	}
	if links[1].Closing {
		t.Fatalf("expected a bare mention not to close, got %#v", links[1])
	}
	for _, link := range links {
		if link.Source != model.IssueLinkAuto {
			t.Fatalf("expected an auto link, got %#v", link)
		}
	}
}

// Providers redeliver webhooks; a redelivery must not stack duplicate links.
func TestLinkIssuesFromEventIsIdempotent(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "fixed", model.IssueInProgress)
	event := &webhook.Event{Commits: []webhook.Commit{{ID: "c1", Message: "fixes ENG-" + itoa(issue.Number)}}}

	svc.LinkIssuesFromEvent(context.Background(), 5, event)
	svc.LinkIssuesFromEvent(context.Background(), 5, event)

	var count int64
	if err := svc.DB.Model(&model.IssueDeployLink{}).Where("task_id = ?", 5).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 link after a redelivery, got %d", count)
	}
}

func TestCloseIssuesForTask(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	closing := newIssue(t, svc, board.ID, "closing", model.IssueInProgress)
	mentioned := newIssue(t, svc, board.ID, "mentioned", model.IssueTodo)
	already := newIssue(t, svc, board.ID, "already canceled", model.IssueCanceled)

	links := []model.IssueDeployLink{
		{IssueID: closing.ID, TaskID: 9, Closing: true, Source: model.IssueLinkAuto},
		{IssueID: mentioned.ID, TaskID: 9, Closing: false, Source: model.IssueLinkAuto},
		{IssueID: already.ID, TaskID: 9, Closing: true, Source: model.IssueLinkAuto},
	}
	if err := svc.DB.Create(&links).Error; err != nil {
		t.Fatal(err)
	}

	svc.CloseIssuesForTask(context.Background(), 9)

	if got := reloadIssue(t, svc, closing.ID); got.Status != model.IssueDone || got.CompletedAt == nil {
		t.Fatalf("expected the closing issue to be done, got %#v", got)
	}
	if got := reloadIssue(t, svc, mentioned.ID); got.Status != model.IssueTodo {
		t.Fatalf("expected a bare mention to be left alone, got %s", got.Status)
	}
	// An issue someone already canceled is not resurrected into Done.
	if got := reloadIssue(t, svc, already.ID); got.Status != model.IssueCanceled {
		t.Fatalf("expected a canceled issue to stay canceled, got %s", got.Status)
	}
}

// reloadIssue reads an issue into a fresh struct: GORM treats a primary key
// already set on the destination as an extra query condition, so reusing one
// variable across reloads silently stops matching.
func reloadIssue(t *testing.T, svc *Service, id uint64) model.Issue {
	t.Helper()
	var issue model.Issue
	if err := svc.DB.First(&issue, id).Error; err != nil {
		t.Fatal(err)
	}
	return issue
}

func TestDeleteBoardRefusesWhileIssuesRemain(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "x", model.IssueTodo)

	if err := svc.DeleteBoard(context.Background(), board.ID); err != ErrBoardHasIssues {
		t.Fatalf("expected ErrBoardHasIssues, got %v", err)
	}
	if err := svc.DeleteIssue(context.Background(), issue.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBoard(context.Background(), board.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBoard(context.Background(), board.ID); err != ErrBoardNotFound {
		t.Fatalf("expected ErrBoardNotFound, got %v", err)
	}
}

func TestDeleteIssueRemovesItsDeployLinks(t *testing.T) {
	svc := newBoardService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "x", model.IssueTodo)
	if err := svc.DB.Create(&model.IssueDeployLink{IssueID: issue.ID, TaskID: 3, Source: model.IssueLinkAuto}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteIssue(context.Background(), issue.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := svc.DB.Model(&model.IssueDeployLink{}).Where("issue_id = ?", issue.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected the issue's links to be removed, got %d", count)
	}
}

func equalIDs(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
