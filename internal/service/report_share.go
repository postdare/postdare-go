package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/hellodeveye/postdare-go/internal/model"
	"gorm.io/gorm"
)

var ErrReportNotFound = errors.New("report not found")

func (s *Service) RotateReportShare(ctx context.Context, reportID uint64) (model.Report, string, string, error) {
	var report model.Report
	if err := s.DB.WithContext(ctx).First(&report, reportID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return report, "", "", ErrReportNotFound
		}
		return report, "", "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return report, "", "", fmt.Errorf("generate report token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	report.ShareTokenHash = hex.EncodeToString(hash[:])
	report.ShareEnabled = true
	if err := s.DB.WithContext(ctx).Model(&report).Updates(map[string]interface{}{
		"share_token_hash": report.ShareTokenHash,
		"share_enabled":    true,
	}).Error; err != nil {
		return report, "", "", err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(s.Config.Server.PublicURL), "/")
	url := fmt.Sprintf("%s/reports/%d#token=%s", baseURL, report.ID, token)
	if baseURL == "" {
		url = fmt.Sprintf("/reports/%d#token=%s", report.ID, token)
	}
	return report, token, url, nil
}

func (s *Service) RevokeReportShare(ctx context.Context, reportID uint64) error {
	result := s.DB.WithContext(ctx).Model(&model.Report{}).Where("id = ?", reportID).Updates(map[string]interface{}{
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

func HashReportToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
