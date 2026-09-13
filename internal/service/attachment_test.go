package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
)

func newAttachmentService(t *testing.T) *Service {
	t.Helper()
	svc := newBoardService(t)
	if err := svc.DB.AutoMigrate(&model.Attachment{}); err != nil {
		t.Fatal(err)
	}
	svc.Config.DataDir = t.TempDir()
	return svc
}

// A one-pixel PNG: enough bytes for the sniffer to recognise the format.
func pngBytes() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89,
	}
}

func gifBytes() []byte { return append([]byte("GIF89a"), bytes.Repeat([]byte{0x00}, 16)...) }
func jpegBytes() []byte {
	return append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte{0x00}, 16)...)
}

func webpBytes() []byte {
	data := []byte("RIFF")
	data = append(data, 0x20, 0x00, 0x00, 0x00)
	data = append(data, []byte("WEBPVP8 ")...)
	return append(data, bytes.Repeat([]byte{0x00}, 16)...)
}

func TestSaveAttachmentAcceptsRasterImages(t *testing.T) {
	svc := newAttachmentService(t)
	cases := map[string][]byte{"image/png": pngBytes(), "image/jpeg": jpegBytes(), "image/gif": gifBytes(), "image/webp": webpBytes()}
	for wantType, data := range cases {
		attachment, err := svc.SaveAttachment(context.Background(), bytes.NewReader(data), "shot.png", nil)
		if err != nil {
			t.Fatalf("%s: %v", wantType, err)
		}
		if attachment.ContentType != wantType {
			t.Fatalf("expected %s, got %s", wantType, attachment.ContentType)
		}
		if attachment.Size != int64(len(data)) {
			t.Fatalf("expected size %d, got %d", len(data), attachment.Size)
		}
		path := filepath.Join(svc.Config.AttachmentDir(), attachment.StoredName)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s: stored file missing: %v", wantType, err)
		}
	}
}

// An SVG served from this origin can run script, and the session token lives in
// the browser's storage, so accepting one would be an account takeover for
// anyone who opens the issue. This is the test that must never be relaxed.
func TestSaveAttachmentRejectsSVG(t *testing.T) {
	svc := newAttachmentService(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, err := svc.SaveAttachment(context.Background(), bytes.NewReader(svg), "logo.svg", nil); err != ErrAttachmentType {
		t.Fatalf("expected ErrAttachmentType, got %v", err)
	}
	entries, err := os.ReadDir(svc.Config.AttachmentDir())
	if err == nil && len(entries) > 0 {
		t.Fatalf("a refused upload must not reach the disk, found %d files", len(entries))
	}
}

// The name and the declared type are both chosen by the uploader, so neither may
// decide what the file is served as -- only the bytes may.
func TestSaveAttachmentIgnoresClaimedNameAndType(t *testing.T) {
	svc := newAttachmentService(t)
	script := []byte("<!doctype html><script>alert(1)</script>")
	if _, err := svc.SaveAttachment(context.Background(), bytes.NewReader(script), "totally.png", nil); err != ErrAttachmentType {
		t.Fatalf("expected HTML named .png to be refused, got %v", err)
	}

	attachment, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "screenshot.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.ContentType != "image/png" || !strings.HasSuffix(attachment.StoredName, ".png") {
		t.Fatalf("expected the bytes to decide the type, got %s / %s", attachment.ContentType, attachment.StoredName)
	}
}

// The stored name is generated, so an upload name cannot choose where the bytes
// land or collide with another upload.
func TestSaveAttachmentGeneratesItsOwnStoredName(t *testing.T) {
	svc := newAttachmentService(t)
	first, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "../../etc/passwd", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "../../etc/passwd", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, attachment := range []*model.Attachment{first, second} {
		if strings.ContainsAny(attachment.StoredName, `/\`) || strings.Contains(attachment.StoredName, "..") {
			t.Fatalf("stored name is not a bare filename: %q", attachment.StoredName)
		}
	}
	if first.StoredName == second.StoredName {
		t.Fatalf("two uploads shared a stored name: %q", first.StoredName)
	}
	if first.Filename != "passwd" {
		t.Fatalf("expected the display name to be stripped to its base, got %q", first.Filename)
	}
}

func TestSaveAttachmentRejectsEmptyAndOversized(t *testing.T) {
	svc := newAttachmentService(t)
	if _, err := svc.SaveAttachment(context.Background(), bytes.NewReader(nil), "empty.png", nil); err != ErrAttachmentEmpty {
		t.Fatalf("expected ErrAttachmentEmpty, got %v", err)
	}
	huge := append(pngBytes(), bytes.Repeat([]byte{0x00}, MaxAttachmentBytes)...)
	if _, err := svc.SaveAttachment(context.Background(), bytes.NewReader(huge), "huge.png", nil); err == nil {
		t.Fatal("expected an oversized upload to be refused")
	}
}

func TestOpenAttachmentConfinesPath(t *testing.T) {
	svc := newAttachmentService(t)
	attachment, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "shot.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, data, err := svc.OpenAttachment(context.Background(), attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContentType != "image/png" || !bytes.Equal(data, pngBytes()) {
		t.Fatalf("unexpected attachment content")
	}

	// A row whose stored name has been tampered with must not read outside the
	// attachment directory.
	secret := filepath.Join(filepath.Dir(svc.Config.AttachmentDir()), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.DB.Model(&model.Attachment{}).Where("id = ?", attachment.ID).Update("stored_name", "../secret.txt").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.OpenAttachment(context.Background(), attachment.ID); err != ErrAttachmentNotFound {
		t.Fatalf("expected a traversal to be refused, got %v", err)
	}

	if _, _, err := svc.OpenAttachment(context.Background(), 9999); err != ErrAttachmentNotFound {
		t.Fatalf("expected ErrAttachmentNotFound, got %v", err)
	}
}

func TestBindAttachmentsFollowsTheDescription(t *testing.T) {
	svc := newAttachmentService(t)
	board := newBoard(t, svc, "ENG")
	used, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "used.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	unused, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "unused.png", nil)
	if err != nil {
		t.Fatal(err)
	}

	issue := model.Issue{
		Title:       "with a screenshot",
		Description: "before\n\n![shot](/api/v1/attachments/" + itoa(used.ID) + ")\n\nafter",
	}
	if err := svc.CreateIssue(context.Background(), board.ID, &issue); err != nil {
		t.Fatal(err)
	}

	if got := reloadAttachment(t, svc, used.ID); got.IssueID == nil || *got.IssueID != issue.ID {
		t.Fatalf("expected the referenced attachment to be bound, got %#v", got.IssueID)
	}
	if got := reloadAttachment(t, svc, unused.ID); got.IssueID != nil {
		t.Fatalf("expected an unreferenced attachment to stay unbound")
	}
}

// Quoting another issue's image must not steal it, or deleting the quoting
// issue would break the original.
func TestBindAttachmentsDoesNotStealFromAnotherIssue(t *testing.T) {
	svc := newAttachmentService(t)
	board := newBoard(t, svc, "ENG")
	attachment, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "shot.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	reference := "![shot](/api/v1/attachments/" + itoa(attachment.ID) + ")"

	owner := model.Issue{Title: "owner", Description: reference}
	if err := svc.CreateIssue(context.Background(), board.ID, &owner); err != nil {
		t.Fatal(err)
	}
	quoter := model.Issue{Title: "quoter", Description: "see " + reference}
	if err := svc.CreateIssue(context.Background(), board.ID, &quoter); err != nil {
		t.Fatal(err)
	}

	if got := reloadAttachment(t, svc, attachment.ID); got.IssueID == nil || *got.IssueID != owner.ID {
		t.Fatalf("expected the attachment to stay with its first issue, got %#v", got.IssueID)
	}
}

func TestDeleteIssueRemovesItsAttachments(t *testing.T) {
	svc := newAttachmentService(t)
	board := newBoard(t, svc, "ENG")
	attachment, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "shot.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	issue := model.Issue{Title: "x", Description: "![shot](/api/v1/attachments/" + itoa(attachment.ID) + ")"}
	if err := svc.CreateIssue(context.Background(), board.ID, &issue); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(svc.Config.AttachmentDir(), attachment.StoredName)

	if err := svc.DeleteIssue(context.Background(), issue.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := svc.DB.Model(&model.Attachment{}).Where("id = ?", attachment.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expected the attachment row to be removed with the issue")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the file to be removed, stat error was %v", err)
	}
}

func TestSweepOrphanAttachments(t *testing.T) {
	svc := newAttachmentService(t)
	board := newBoard(t, svc, "ENG")

	fresh, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "fresh.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "stale.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := svc.SaveAttachment(context.Background(), bytes.NewReader(pngBytes()), "bound.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	issue := model.Issue{Title: "x", Description: "![b](/api/v1/attachments/" + itoa(bound.ID) + ")"}
	if err := svc.CreateIssue(context.Background(), board.ID, &issue); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-48 * time.Hour)
	for _, id := range []uint64{stale.ID, bound.ID} {
		if err := svc.DB.Model(&model.Attachment{}).Where("id = ?", id).Update("created_at", old).Error; err != nil {
			t.Fatal(err)
		}
	}

	swept, err := svc.SweepOrphanAttachments(context.Background(), OrphanAttachmentAge)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("expected only the stale unbound upload to be swept, got %d", swept)
	}
	// A pasted image whose issue is still being written must survive.
	if got := countAttachments(t, svc, fresh.ID); got != 1 {
		t.Fatal("a recent unbound upload was swept")
	}
	// And one that belongs to an issue must survive however old it is.
	if got := countAttachments(t, svc, bound.ID); got != 1 {
		t.Fatal("an attachment belonging to an issue was swept")
	}
	if got := countAttachments(t, svc, stale.ID); got != 0 {
		t.Fatal("the stale unbound upload was not swept")
	}
}

func reloadAttachment(t *testing.T, svc *Service, id uint64) model.Attachment {
	t.Helper()
	var attachment model.Attachment
	if err := svc.DB.First(&attachment, id).Error; err != nil {
		t.Fatal(err)
	}
	return attachment
}

func countAttachments(t *testing.T, svc *Service, id uint64) int64 {
	t.Helper()
	var count int64
	if err := svc.DB.Model(&model.Attachment{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
