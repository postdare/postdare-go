package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hellodeveye/postdare-go/internal/model"
)

func newCommentService(t *testing.T) *Service {
	t.Helper()
	svc := newBoardService(t)
	if err := svc.DB.AutoMigrate(&model.IssueComment{}, &model.Attachment{}); err != nil {
		t.Fatal(err)
	}
	svc.Config.DataDir = t.TempDir()
	return svc
}

func TestCommentsReadBackInTheOrderTheyWereWritten(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "login fails", "")
	author := uint64(7)

	for _, body := range []string{"first", "second", "third"} {
		if _, err := svc.CreateIssueComment(context.Background(), issue.ID, &author, body); err != nil {
			t.Fatal(err)
		}
	}

	comments, err := svc.ListIssueComments(context.Background(), issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	bodies := make([]string, 0, len(comments))
	for _, comment := range comments {
		bodies = append(bodies, comment.Body)
	}
	if strings.Join(bodies, ",") != "first,second,third" {
		t.Fatalf("thread reads %v, want first,second,third", bodies)
	}
}

func TestCommentBodyIsRequired(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "login fails", "")

	// The editor serializes an empty document as a newline, so whitespace has
	// to be refused the same way an empty string is.
	if _, err := svc.CreateIssueComment(context.Background(), issue.ID, nil, "  \n "); !errors.Is(err, ErrCommentEmpty) {
		t.Fatalf("empty comment error is %v, want ErrCommentEmpty", err)
	}
	long := strings.Repeat("x", model.MaxIssueCommentLength+1)
	if _, err := svc.CreateIssueComment(context.Background(), issue.ID, nil, long); !errors.Is(err, ErrCommentTooLong) {
		t.Fatalf("oversized comment error is %v, want ErrCommentTooLong", err)
	}
}

func TestCommentOnMissingIssueIsRefused(t *testing.T) {
	svc := newCommentService(t)
	if _, err := svc.CreateIssueComment(context.Background(), 404, nil, "hello"); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("error is %v, want ErrIssueNotFound", err)
	}
}

func TestUpdateAndDeleteComment(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "login fails", "")

	comment, err := svc.CreateIssueComment(context.Background(), issue.ID, nil, "looks like a cache")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateIssueComment(context.Background(), comment.ID, "looks like the session cache"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := svc.LoadIssueComment(context.Background(), comment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Body != "looks like the session cache" {
		t.Fatalf("body is %q after the edit", reloaded.Body)
	}
	if err := svc.DeleteIssueComment(context.Background(), comment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoadIssueComment(context.Background(), comment.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Fatalf("error after delete is %v, want ErrCommentNotFound", err)
	}
}

// A deleted issue takes its thread with it: a comment that outlived its card
// would be unreachable and would still be counted by anything that groups by
// issue id.
func TestDeleteIssueClearsItsThread(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "login fails", "")
	kept := newIssue(t, svc, board.ID, "signup fails", "")

	if _, err := svc.CreateIssueComment(context.Background(), issue.ID, nil, "on it"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateIssueComment(context.Background(), kept.ID, nil, "not this one"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteIssue(context.Background(), issue.ID); err != nil {
		t.Fatal(err)
	}

	var remaining int64
	if err := svc.DB.Model(&model.IssueComment{}).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("%d comments remain, want only the other issue's", remaining)
	}
}

// A screenshot pasted into a comment belongs to the issue, so deleting the
// issue sweeps it along with the description's images.
func TestCommentBindsItsAttachmentsToTheIssue(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	issue := newIssue(t, svc, board.ID, "login fails", "")

	attachment, err := svc.SaveAttachment(context.Background(), bytesReader(pngBytes()), "shot.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := "here it is\n\n![shot](/api/v1/attachments/" + itoa(attachment.ID) + ")"
	if _, err := svc.CreateIssueComment(context.Background(), issue.ID, nil, body); err != nil {
		t.Fatal(err)
	}

	var stored model.Attachment
	if err := svc.DB.First(&stored, attachment.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.IssueID == nil || *stored.IssueID != issue.ID {
		t.Fatalf("attachment is bound to %v, want issue %d", stored.IssueID, issue.ID)
	}
}

func TestCountIssueCommentsGroupsByIssue(t *testing.T) {
	svc := newCommentService(t)
	board := newBoard(t, svc, "ENG")
	busy := newIssue(t, svc, board.ID, "login fails", "")
	quiet := newIssue(t, svc, board.ID, "signup fails", "")

	for _, body := range []string{"one", "two"} {
		if _, err := svc.CreateIssueComment(context.Background(), busy.ID, nil, body); err != nil {
			t.Fatal(err)
		}
	}

	counts := svc.CountIssueComments(context.Background(), []uint64{busy.ID, quiet.ID})
	if counts[busy.ID] != 2 {
		t.Fatalf("busy issue counted %d, want 2", counts[busy.ID])
	}
	if _, ok := counts[quiet.ID]; ok {
		t.Fatalf("an issue with no comments should be absent, got %d", counts[quiet.ID])
	}
}

func bytesReader(data []byte) *strings.Reader { return strings.NewReader(string(data)) }
