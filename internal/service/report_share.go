package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/hellodeveye/postdare-go/internal/model"
	"gorm.io/gorm"
)

var ErrReportNotFound = errors.New("report not found")

// reportShareTokenPurpose keeps share tokens domain-separated from anything else
// derived from the server secret.
const reportShareTokenPurpose = "postdare-go/report-share/v1"

// EnsureReportShare enables sharing for a report and returns its link, reusing
// the token already in force when there is one.
//
// Anything that merely needs a link to hand out -- a notification card, an
// embedded button -- must use this rather than RotateReportShare. Rotating on
// every send would silently invalidate links already delivered to a chat channel,
// so a task with two notification stages would leave the first card's button dead.
func (s *Service) EnsureReportShare(ctx context.Context, reportID uint64) (model.Report, string, error) {
	report, err := s.loadReport(ctx, reportID)
	if err != nil {
		return report, "", err
	}
	if report.ShareEnabled && report.ShareSalt != "" {
		return report, s.reportShareURL(report.ID, s.reportShareToken(report.ID, report.ShareSalt)), nil
	}
	token, err := s.enableReportShare(ctx, &report)
	if err != nil {
		return report, "", err
	}
	return report, s.reportShareURL(report.ID, token), nil
}

// RotateReportShare draws a new salt, so every link handed out earlier stops
// working. It backs the explicit "share" action and nothing else.
func (s *Service) RotateReportShare(ctx context.Context, reportID uint64) (model.Report, string, string, error) {
	report, err := s.loadReport(ctx, reportID)
	if err != nil {
		return report, "", "", err
	}
	token, err := s.enableReportShare(ctx, &report)
	if err != nil {
		return report, "", "", err
	}
	return report, token, s.reportShareURL(report.ID, token), nil
}

func (s *Service) RevokeReportShare(ctx context.Context, reportID uint64) error {
	result := s.DB.WithContext(ctx).Model(&model.Report{}).Where("id = ?", reportID).Updates(map[string]interface{}{
		"share_salt":       "",
		"share_token_hash": "",
		"share_enabled":    false,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrReportNotFound
	}
	return nil
}

func (s *Service) loadReport(ctx context.Context, reportID uint64) (model.Report, error) {
	var report model.Report
	if err := s.DB.WithContext(ctx).First(&report, reportID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return report, ErrReportNotFound
		}
		return report, err
	}
	return report, nil
}

// enableReportShare draws a fresh salt and persists it alongside the digest of
// the token it derives, returning the raw token to the caller.
func (s *Service) enableReportShare(ctx context.Context, report *model.Report) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate report share salt: %w", err)
	}
	salt := hex.EncodeToString(raw)
	token := s.reportShareToken(report.ID, salt)
	report.ShareSalt = salt
	report.ShareTokenHash = HashReportToken(token)
	report.ShareEnabled = true
	if err := s.DB.WithContext(ctx).Model(report).Updates(map[string]interface{}{
		"share_salt":       report.ShareSalt,
		"share_token_hash": report.ShareTokenHash,
		"share_enabled":    true,
	}).Error; err != nil {
		return "", err
	}
	return token, nil
}

// reportShareToken derives a report's bearer token from the server secret and the
// report's stored salt. Deriving rather than storing keeps the raw token out of
// the database -- a database copy alone yields no working link -- while still
// letting EnsureReportShare reproduce the same link on every notification.
func (s *Service) reportShareToken(reportID uint64, salt string) string {
	mac := hmac.New(sha256.New, []byte(s.Config.JWT.Secret))
	_, _ = io.WriteString(mac, reportShareTokenPurpose+"\x00"+strconv.FormatUint(reportID, 10)+"\x00"+salt)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// reportShareURL builds the public report link. With no public_url configured it
// falls back to a site-relative path, which still works when the operator opens
// it on the same origin.
func (s *Service) reportShareURL(reportID uint64, token string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(s.Config.Server.PublicURL), "/")
	return fmt.Sprintf("%s/reports/%d#token=%s", baseURL, reportID, token)
}

// HashReportToken returns the digest stored for a share token; the public
// endpoint looks a report up by it.
func HashReportToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
