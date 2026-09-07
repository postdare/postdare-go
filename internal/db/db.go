package db

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/model"
)

type Option func(*options)

type options struct {
	adminPassword           string
	generatedPasswordLogger func(password string)
}

func WithAdminPassword(password string) Option {
	return func(opts *options) {
		opts.adminPassword = password
	}
}

func WithGeneratedPasswordLogger(logger func(password string)) Option {
	return func(opts *options) {
		opts.generatedPasswordLogger = logger
	}
}

func Open(cfg config.DatabaseConfig, opts ...Option) (*gorm.DB, error) {
	openOpts := options{}
	for _, opt := range opts {
		opt(&openOpts)
	}
	dialector, err := dialector(cfg)
	if err != nil {
		return nil, err
	}
	database, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.Driver, err)
	}
	if err := database.AutoMigrate(
		&model.User{},
		&model.Project{},
		&model.DeployTask{},
		&model.DeployTaskStage{},
		&model.Report{},
		&model.WebhookEvent{},
		&model.Setting{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	if err := seedDefaultAdmin(database, openOpts); err != nil {
		return nil, err
	}
	return database, nil
}

func dialector(cfg config.DatabaseConfig) (gorm.Dialector, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "sqlite":
		if cfg.Path == "" {
			return nil, fmt.Errorf("database.path is required")
		}
		if err := ensureSQLiteDir(cfg.Path); err != nil {
			return nil, err
		}
		return sqlite.Open(sqliteDSN(cfg.Path)), nil
	case "mysql":
		if cfg.DSN == "" {
			return nil, fmt.Errorf("database.dsn is required")
		}
		return mysql.Open(cfg.DSN), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
}

func ensureSQLiteDir(path string) error {
	if path == ":memory:" || strings.HasPrefix(path, "file:") {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory: %w", err)
	}
	return nil
}

func sqliteDSN(path string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

func seedDefaultAdmin(database *gorm.DB, opts options) error {
	var count int64
	if err := database.Model(&model.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		return fmt.Errorf("count default admin: %w", err)
	}
	if count > 0 {
		return nil
	}
	password := opts.adminPassword
	mustChangePassword := false
	if password == "" {
		var err error
		password, err = randomPassword()
		if err != nil {
			return fmt.Errorf("generate default admin password: %w", err)
		}
		mustChangePassword = true
		if opts.generatedPasswordLogger != nil {
			opts.generatedPasswordLogger(password)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default admin password: %w", err)
	}
	user := model.User{Username: "admin", PasswordHash: string(hash), Role: "admin", MustChangePassword: mustChangePassword}
	if err := database.Create(&user).Error; err != nil {
		return fmt.Errorf("create default admin: %w", err)
	}
	return nil
}

func randomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
