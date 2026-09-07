package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/sse"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "postdare-go-test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&model.Project{}, &model.DeployTask{}, &model.DeployTaskStage{}, &model.Report{}); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Deploy.LogDir = t.TempDir()
	cfg.Deploy.CommandTimeoutMinutes = 1
	cfg.AppLog.MaxTailLines = 500
	cfg.AppLog.MaxAllowedLines = 5000
	svc := New(database, cfg, sse.NewHub(), zap.NewNop())
	return svc
}

func commandStage(name string, command string, enabled bool) model.ProjectStage {
	return model.ProjectStage{
		Name:    name,
		Type:    model.ProjectStageTypeCommand,
		Enabled: enabled,
		Config:  rawStageConfig(model.CommandStageConfig{Command: command}),
	}
}

func commandStageWithPolicy(name string, command string, runWhen string, continueOnError bool) model.ProjectStage {
	stage := commandStage(name, command, true)
	stage.RunWhen = runWhen
	stage.ContinueOnError = continueOnError
	return stage
}

func healthCheckStage(name string, url string, runWhen string) model.ProjectStage {
	return model.ProjectStage{
		Name:    name,
		Type:    model.ProjectStageTypeHealthCheck,
		Enabled: true,
		RunWhen: runWhen,
		Config:  rawStageConfig(model.HealthCheckStageConfig{URL: url}),
	}
}

func outboundWebhookStage(name string, url string, runWhen string) model.ProjectStage {
	return model.ProjectStage{
		Name:            name,
		Type:            model.ProjectStageTypeOutboundWebhook,
		Enabled:         true,
		RunWhen:         runWhen,
		ContinueOnError: true,
		Config: rawStageConfig(model.OutboundWebhookStageConfig{
			URL:      url,
			Template: "dingtalk_text",
		}),
	}
}

func rawStageConfig(value interface{}) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}
