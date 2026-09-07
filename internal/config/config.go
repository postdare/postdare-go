package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultDataDir      = "/data/postdare-go"
	DefaultDatabasePath = "/data/postdare-go/postdare.db"
)

type Config struct {
	DataDir  string         `yaml:"data_dir"`
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	JWT      JWTConfig      `yaml:"jwt"`
	Deploy   DeployConfig   `yaml:"deploy"`
	AppLog   AppLogConfig   `yaml:"app_log"`
	MCP      MCPConfig      `yaml:"mcp"`

	AdminPassword string `yaml:"-"`
}

type ServerConfig struct {
	Port        int      `yaml:"port"`
	CORSOrigins []string `yaml:"cors_origins"`
	PublicURL   string   `yaml:"public_url"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	Path   string `yaml:"path"`
	DSN    string `yaml:"dsn"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type DeployConfig struct {
	LogDir                string `yaml:"log_dir"`
	CommandTimeoutMinutes int    `yaml:"command_timeout_minutes"`
}

type AppLogConfig struct {
	MaxTailLines    int `yaml:"max_tail_lines"`
	MaxAllowedLines int `yaml:"max_allowed_lines"`
}

type MCPConfig struct {
	Enabled            bool   `yaml:"enabled"`
	AllowMutationTools bool   `yaml:"allow_mutation_tools"`
	APIToken           string `yaml:"api_token"`
}

func Load(path string) (*Config, error) {
	raw, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}
	cfg.applyDefaults()
	if err := cfg.applyEnvironment(); err != nil {
		return nil, err
	}
	if err := cfg.resolveSecrets(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func readConfig(path string) ([]byte, error) {
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		return raw, nil
	}
	seen := map[string]bool{}
	for _, candidate := range defaultConfigPaths() {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		raw, err := os.ReadFile(candidate)
		if err == nil {
			return raw, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	return nil, nil
}

func defaultConfigPaths() []string {
	paths := []string{"config.yaml"}
	executable, err := os.Executable()
	if err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), "config.yaml"))
	}
	return paths
}

func (c *Config) applyDefaults() {
	if c.DataDir == "" {
		c.DataDir = DefaultDataDir
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8088
	}
	if c.Database.Driver == "" {
		c.Database.Driver = "sqlite"
	}
	if c.Database.Path == "" {
		c.Database.Path = filepath.Join(c.DataDir, "postdare.db")
	}
	if c.JWT.ExpireHours == 0 {
		c.JWT.ExpireHours = 72
	}
	if c.Deploy.LogDir == "" {
		c.Deploy.LogDir = filepath.Join(c.DataDir, "logs", "deploy")
	}
	if c.Deploy.CommandTimeoutMinutes == 0 {
		c.Deploy.CommandTimeoutMinutes = 30
	}
	if c.AppLog.MaxTailLines == 0 {
		c.AppLog.MaxTailLines = 500
	}
	if c.AppLog.MaxAllowedLines == 0 {
		c.AppLog.MaxAllowedLines = 5000
	}
}

func (c *Config) applyEnvironment() error {
	var err error
	if raw := os.Getenv("POSTDARE_GO_DATA_DIR"); raw != "" {
		c.DataDir = raw
		if c.Database.Path == DefaultDatabasePath {
			c.Database.Path = filepath.Join(c.DataDir, "postdare.db")
		}
		if c.Deploy.LogDir == filepath.Join(DefaultDataDir, "logs", "deploy") {
			c.Deploy.LogDir = filepath.Join(c.DataDir, "logs", "deploy")
		}
	}
	if raw := os.Getenv("POSTDARE_GO_PORT"); raw != "" {
		c.Server.Port, err = strconv.Atoi(raw)
		if err != nil || c.Server.Port <= 0 {
			return fmt.Errorf("POSTDARE_GO_PORT must be a positive integer")
		}
	}
	if raw := os.Getenv("POSTDARE_GO_DB_DRIVER"); raw != "" {
		c.Database.Driver = raw
	}
	if raw := os.Getenv("POSTDARE_GO_DB_PATH"); raw != "" {
		c.Database.Path = raw
	}
	if raw := os.Getenv("POSTDARE_GO_DB_DSN"); raw != "" {
		c.Database.DSN = raw
	}
	if raw := os.Getenv("POSTDARE_GO_JWT_SECRET"); raw != "" {
		c.JWT.Secret = raw
	}
	if raw := os.Getenv("POSTDARE_GO_MCP_API_TOKEN"); raw != "" {
		c.MCP.APIToken = raw
	}
	if raw := os.Getenv("POSTDARE_GO_ADMIN_PASSWORD"); raw != "" {
		c.AdminPassword = raw
	}
	return nil
}

func (c *Config) resolveSecrets() error {
	envJWT := os.Getenv("POSTDARE_GO_JWT_SECRET") != ""
	envMCP := os.Getenv("POSTDARE_GO_MCP_API_TOKEN") != ""
	needsJWT := !envJWT && isPlaceholderSecret(c.JWT.Secret)
	needsMCP := !envMCP && isPlaceholderSecret(c.MCP.APIToken)
	if !needsJWT && !needsMCP {
		return nil
	}
	secretsPath := filepath.Join(c.DataDir, "secrets.yaml")
	secrets, err := readSecretsFile(secretsPath)
	if err != nil {
		return err
	}
	changed := false
	if needsJWT {
		if !isPlaceholderSecret(secrets.JWT.Secret) {
			c.JWT.Secret = secrets.JWT.Secret
		} else {
			secret, err := randomHex(32)
			if err != nil {
				return fmt.Errorf("generate jwt secret: %w", err)
			}
			c.JWT.Secret = secret
			secrets.JWT.Secret = secret
			changed = true
		}
	}
	if needsMCP {
		if !isPlaceholderSecret(secrets.MCP.APIToken) {
			c.MCP.APIToken = secrets.MCP.APIToken
		} else {
			secret, err := randomHex(32)
			if err != nil {
				return fmt.Errorf("generate mcp api token: %w", err)
			}
			c.MCP.APIToken = secret
			secrets.MCP.APIToken = secret
			changed = true
		}
	}
	if changed {
		if err := writeSecretsFile(secretsPath, secrets); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validate() error {
	c.Server.PublicURL = strings.TrimRight(strings.TrimSpace(c.Server.PublicURL), "/")
	c.Database.Driver = strings.ToLower(strings.TrimSpace(c.Database.Driver))
	switch c.Database.Driver {
	case "sqlite":
		if strings.TrimSpace(c.Database.Path) == "" {
			return fmt.Errorf("database.path is required when database.driver=sqlite")
		}
	case "mysql":
		if strings.TrimSpace(c.Database.DSN) == "" {
			return fmt.Errorf("database.dsn is required when database.driver=mysql")
		}
	default:
		return fmt.Errorf("database.driver must be sqlite or mysql")
	}
	if isPlaceholderSecret(c.JWT.Secret) {
		return fmt.Errorf("jwt.secret could not be resolved")
	}
	if isPlaceholderSecret(c.MCP.APIToken) {
		return fmt.Errorf("mcp.api_token could not be resolved")
	}
	return nil
}

type secretsFile struct {
	JWT struct {
		Secret string `yaml:"secret"`
	} `yaml:"jwt"`
	MCP struct {
		APIToken string `yaml:"api_token"`
	} `yaml:"mcp"`
}

func readSecretsFile(path string) (secretsFile, error) {
	var secrets secretsFile
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return secrets, nil
		}
		return secrets, fmt.Errorf("read secrets: %w", err)
	}
	if err := yaml.Unmarshal(raw, &secrets); err != nil {
		return secrets, fmt.Errorf("parse secrets: %w", err)
	}
	return secrets, nil
}

func writeSecretsFile(path string, secrets secretsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	raw, err := yaml.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write secrets: %w", err)
	}
	return nil
}

func isPlaceholderSecret(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.HasPrefix(value, "please-change-this-")
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (c *Config) JWTDuration() time.Duration {
	return time.Duration(c.JWT.ExpireHours) * time.Hour
}

func (c *Config) CommandTimeout() time.Duration {
	return time.Duration(c.Deploy.CommandTimeoutMinutes) * time.Minute
}
