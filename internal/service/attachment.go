package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hellodeveye/postdare-go/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrAttachmentNotFound = errors.New("attachment not found")
	ErrAttachmentType     = errors.New("attachment must be a PNG, JPEG, GIF or WebP image")
	ErrAttachmentEmpty    = errors.New("attachment is empty")
)

// MaxAttachmentBytes caps one upload. Screenshots are the case this exists for,
// and a screenshot that does not fit in 10MB is a screen recording.
const MaxAttachmentBytes = 10 << 20

// OrphanAttachmentAge is how long an upload may stay unbound before it is swept.
// An image is uploaded while its issue is still being written, so a row with no
// issue is normal for minutes; a day later it means the issue was never saved.
const OrphanAttachmentAge = 24 * time.Hour

// attachmentRefPattern finds the attachments a description refers to. It matches
// the URL this server hands out, so an arbitrary link in the markdown -- to a
// site, or to another kind of endpoint -- binds nothing.
var attachmentRefPattern = regexp.MustCompile(`/api/v1/attachments/(\d+)`)

// SaveAttachment stores an uploaded image and records it.
//
// The content type is decided here by sniffing the leading bytes, never taken
// from the request's declared type or the file's name: both are chosen by the
// uploader, and the stored type is what the download response will later
// declare. A file whose bytes are not one of the allowed raster formats is
// refused before anything touches the disk.
func (s *Service) SaveAttachment(ctx context.Context, source io.Reader, filename string, uploaderID *uint64) (*model.Attachment, error) {
	// One byte past the cap is read so an oversized upload is detected rather
	// than silently truncated into a corrupt image.
	data, err := io.ReadAll(io.LimitReader(source, MaxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrAttachmentEmpty
	}
	if len(data) > MaxAttachmentBytes {
		return nil, fmt.Errorf("attachment is larger than %dMB", MaxAttachmentBytes>>20)
	}
	contentType := sniffImageType(data)
	extension := model.AttachmentExtension(contentType)
	if extension == "" {
		return nil, ErrAttachmentType
	}

	storedName, err := randomStoredName(extension)
	if err != nil {
		return nil, err
	}
	dir := s.Config.AttachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create attachment directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, storedName), data, 0o644); err != nil {
		return nil, fmt.Errorf("write attachment: %w", err)
	}

	attachment := model.Attachment{
		UploaderID:  uploaderID,
		Filename:    safeDisplayName(filename),
		StoredName:  storedName,
		ContentType: contentType,
		Size:        int64(len(data)),
	}
	if err := s.DB.WithContext(ctx).Create(&attachment).Error; err != nil {
		// The row is the record of the file; without it the bytes are unreachable.
		_ = os.Remove(filepath.Join(dir, storedName))
		return nil, err
	}
	return &attachment, nil
}

// OpenAttachment returns the stored file for download, with the path confined to
// the attachment directory so a stored name that somehow carried a traversal
// could not reach outside it.
func (s *Service) OpenAttachment(ctx context.Context, id uint64) (*model.Attachment, []byte, error) {
	var attachment model.Attachment
	if err := s.DB.WithContext(ctx).First(&attachment, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrAttachmentNotFound
		}
		return nil, nil, err
	}
	path, err := attachmentPath(s.Config.AttachmentDir(), attachment.StoredName)
	if err != nil {
		return nil, nil, ErrAttachmentNotFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, ErrAttachmentNotFound
	}
	return &attachment, data, nil
}

// BindAttachments ties the attachments a description refers to to their issue,
// so that deleting the issue can later take its images with it.
//
// Only unbound rows, or rows already bound to this issue, are claimed: quoting
// another issue's image must not move that image's ownership, or deleting the
// quoting issue would break the original.
func (s *Service) BindAttachments(ctx context.Context, issueID uint64, description string) {
	ids := attachmentRefs(description)
	if len(ids) == 0 {
		return
	}
	if err := s.DB.WithContext(ctx).Model(&model.Attachment{}).
		Where("id IN ? AND (issue_id IS NULL OR issue_id = ?)", ids, issueID).
		Update("issue_id", issueID).Error; err != nil {
		s.Logger.Warn("bind issue attachments failed", zap.Uint64("issue_id", issueID), zap.Error(err))
	}
}

// DeleteAttachmentsForIssue removes an issue's images, rows and files both. A
// file that cannot be removed is logged rather than failing the delete: the
// issue is gone either way, and a leftover file is a sweep's problem.
func (s *Service) DeleteAttachmentsForIssue(ctx context.Context, issueID uint64) {
	var attachments []model.Attachment
	if err := s.DB.WithContext(ctx).Where("issue_id = ?", issueID).Find(&attachments).Error; err != nil {
		s.Logger.Warn("load issue attachments failed", zap.Uint64("issue_id", issueID), zap.Error(err))
		return
	}
	if len(attachments) == 0 {
		return
	}
	ids := make([]uint64, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.ID)
	}
	if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.Attachment{}).Error; err != nil {
		s.Logger.Warn("delete issue attachments failed", zap.Uint64("issue_id", issueID), zap.Error(err))
		return
	}
	s.removeAttachmentFiles(attachments)
}

// SweepOrphanAttachments deletes uploads that were never referenced by a saved
// issue. Pasting an image and then abandoning the form is ordinary, so without
// this the attachment directory only ever grows.
func (s *Service) SweepOrphanAttachments(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	var attachments []model.Attachment
	if err := s.DB.WithContext(ctx).Where("issue_id IS NULL AND created_at < ?", cutoff).Find(&attachments).Error; err != nil {
		return 0, err
	}
	if len(attachments) == 0 {
		return 0, nil
	}
	ids := make([]uint64, 0, len(attachments))
	for _, attachment := range attachments {
		ids = append(ids, attachment.ID)
	}
	if err := s.DB.WithContext(ctx).Where("id IN ?", ids).Delete(&model.Attachment{}).Error; err != nil {
		return 0, err
	}
	s.removeAttachmentFiles(attachments)
	return len(attachments), nil
}

func (s *Service) removeAttachmentFiles(attachments []model.Attachment) {
	dir := s.Config.AttachmentDir()
	for _, attachment := range attachments {
		path, err := attachmentPath(dir, attachment.StoredName)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			s.Logger.Warn("remove attachment file failed", zap.String("stored_name", attachment.StoredName), zap.Error(err))
		}
	}
}

// sniffImageType reads the format out of the bytes themselves. Go's sniffer
// recognises the raster formats we allow and reports anything else -- an SVG,
// a script, a renamed archive -- as something that is not in the allow list.
func sniffImageType(data []byte) string {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	detected := http.DetectContentType(head)
	if index := strings.IndexByte(detected, ';'); index >= 0 {
		detected = detected[:index]
	}
	return strings.ToLower(strings.TrimSpace(detected))
}

func randomStoredName(extension string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw) + extension, nil
}

// attachmentPath joins a stored name onto the attachment directory and refuses
// anything that does not land inside it.
func attachmentPath(dir string, storedName string) (string, error) {
	if storedName == "" || strings.ContainsAny(storedName, `/\`) {
		return "", fmt.Errorf("invalid stored name")
	}
	base, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, filepath.Clean("/"+storedName))
	if path != filepath.Join(base, storedName) {
		return "", fmt.Errorf("attachment path escapes its directory")
	}
	return path, nil
}

// safeDisplayName keeps an uploader's file name readable without letting it act
// as a path or smuggle control characters into a response header.
func safeDisplayName(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == string(filepath.Separator) {
		filename = ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' || r == '/' {
			return -1
		}
		return r
	}, filename)
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	if cleaned == "" {
		return "image"
	}
	return cleaned
}

func attachmentRefs(description string) []uint64 {
	matches := attachmentRefPattern.FindAllStringSubmatch(description, -1)
	ids := make([]uint64, 0, len(matches))
	seen := map[uint64]bool{}
	for _, match := range matches {
		id, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil || id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}
