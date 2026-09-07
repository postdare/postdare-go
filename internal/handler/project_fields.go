package handler

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hellodeveye/postdare-go/internal/model"
	"github.com/hellodeveye/postdare-go/internal/util"
)

func validateProject(project model.Project) error {
	if strings.TrimSpace(project.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(project.ProjectKey) == "" {
		return fmt.Errorf("project_key is required")
	}
	if project.GitProvider != model.GitProviderGitee && project.GitProvider != model.GitProviderGitHub {
		return fmt.Errorf("git_provider must be gitee or github")
	}
	if strings.TrimSpace(project.AppDir) == "" {
		return fmt.Errorf("app_dir is required")
	}
	if !filepath.IsAbs(project.AppDir) {
		return fmt.Errorf("app_dir must be an absolute path")
	}
	if strings.TrimSpace(project.Branch) == "" {
		return fmt.Errorf("branch is required")
	}
	if project.AppLogPath != "" {
		if _, err := safeProjectAppLogPath(project); err != nil {
			return err
		}
	}
	if err := validateProjectStages(project.Stages); err != nil {
		return err
	}
	return nil
}

func parseProjectStages(value interface{}) ([]model.ProjectStage, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("deploy_stages is invalid: %v", err)
	}
	var stages []model.ProjectStage
	if err := json.Unmarshal(raw, &stages); err != nil {
		return nil, fmt.Errorf("deploy_stages must be an array of typed stage objects: %v", err)
	}
	return stages, validateProjectStages(stages)
}

func validateProjectStages(stages []model.ProjectStage) error {
	for i, st := range stages {
		if strings.TrimSpace(st.Name) == "" {
			return fmt.Errorf("deploy_stages[%d].name is required", i)
		}
		switch st.Type {
		case model.ProjectStageTypeCommand:
			var cfg model.CommandStageConfig
			if err := parseStageConfig(st, &cfg); err != nil {
				return fmt.Errorf("deploy_stages[%d].config is invalid: %v", i, err)
			}
			if st.Enabled && strings.TrimSpace(cfg.Command) == "" {
				return fmt.Errorf("deploy_stages[%d].config.command is required", i)
			}
			if cfg.CaptureAs != "" && cfg.CaptureAs != "report" {
				return fmt.Errorf("deploy_stages[%d].config.capture_as must be report", i)
			}
		case model.ProjectStageTypeHealthCheck:
			var cfg model.HealthCheckStageConfig
			if err := parseStageConfig(st, &cfg); err != nil {
				return fmt.Errorf("deploy_stages[%d].config is invalid: %v", i, err)
			}
			if st.Enabled && strings.TrimSpace(cfg.URL) == "" {
				return fmt.Errorf("deploy_stages[%d].config.url is required", i)
			}
		case model.ProjectStageTypeOutboundWebhook:
			var cfg model.OutboundWebhookStageConfig
			if err := parseStageConfig(st, &cfg); err != nil {
				return fmt.Errorf("deploy_stages[%d].config is invalid: %v", i, err)
			}
			if st.Enabled && strings.TrimSpace(cfg.URL) == "" {
				return fmt.Errorf("deploy_stages[%d].config.url is required", i)
			}
			if cfg.Template != "" && cfg.Template != "dingtalk_text" && cfg.Template != "wecom_text" && cfg.Template != "feishu_text" && cfg.Template != "feishu_report_card" && cfg.Template != "generic_json" {
				return fmt.Errorf("deploy_stages[%d].config.template is unsupported", i)
			}
		default:
			return fmt.Errorf("deploy_stages[%d].type must be command, health_check or outbound_webhook", i)
		}
		switch st.RunWhen {
		case "", model.ProjectStageRunWhenSuccess, model.ProjectStageRunWhenFailed, model.ProjectStageRunWhenAlways:
		default:
			return fmt.Errorf("deploy_stages[%d].run_when must be success, failed or always", i)
		}
	}
	return nil
}

func parseStageConfig(stage model.ProjectStage, out interface{}) error {
	if len(stage.Config) == 0 {
		return nil
	}
	return json.Unmarshal(stage.Config, out)
}

func applyProjectUpdate(project *model.Project, key string, value interface{}) error {
	if key == "deploy_stages" {
		stages, err := parseProjectStages(value)
		if err != nil {
			return err
		}
		if err := preserveMaskedOutboundWebhookURLs(project.Stages, stages); err != nil {
			return err
		}
		project.Stages = stages
		return nil
	}
	stringValue := ""
	if key != "auto_deploy_enabled" {
		if value != nil {
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be a string", key)
			}
			stringValue = s
		}
	}
	switch key {
	case "name":
		project.Name = stringValue
	case "project_key":
		project.ProjectKey = stringValue
	case "git_provider":
		project.GitProvider = stringValue
	case "branch":
		project.Branch = stringValue
	case "app_dir":
		project.AppDir = stringValue
	case "rollback_cmd":
		project.RollbackCmd = stringValue
	case "app_log_path":
		project.AppLogPath = stringValue
	case "webhook_secret":
		project.WebhookSecret = stringValue
	case "auto_deploy_enabled":
		v, ok := value.(bool)
		if !ok {
			return fmt.Errorf("auto_deploy_enabled must be a boolean")
		}
		project.AutoDeployEnabled = v
	}
	return nil
}

func projectUpdateValue(project model.Project, key string) interface{} {
	switch key {
	case "name":
		return project.Name
	case "project_key":
		return project.ProjectKey
	case "git_provider":
		return project.GitProvider
	case "branch":
		return project.Branch
	case "app_dir":
		return project.AppDir
	case "rollback_cmd":
		return project.RollbackCmd
	case "deploy_stages":
		return project.Stages
	case "app_log_path":
		return project.AppLogPath
	case "webhook_secret":
		return project.WebhookSecret
	case "auto_deploy_enabled":
		return project.AutoDeployEnabled
	default:
		return nil
	}
}

func maskProjects(projects []model.Project) []model.Project {
	out := make([]model.Project, len(projects))
	for i, project := range projects {
		out[i] = maskProject(project)
	}
	return out
}

func maskProject(project model.Project) model.Project {
	project.WebhookSecret = util.MaskSecret(project.WebhookSecret)
	for i, stage := range project.Stages {
		if stage.Type != model.ProjectStageTypeOutboundWebhook {
			continue
		}
		var cfg model.OutboundWebhookStageConfig
		if err := parseStageConfig(stage, &cfg); err != nil {
			continue
		}
		if strings.TrimSpace(cfg.URL) == "" {
			continue
		}
		cfg.URL = util.MaskSecret(cfg.URL)
		raw, err := json.Marshal(cfg)
		if err != nil {
			continue
		}
		project.Stages[i].Config = raw
	}
	return project
}

func preserveMaskedOutboundWebhookURLs(existing []model.ProjectStage, next []model.ProjectStage) error {
	used := map[int]bool{}
	for i := range next {
		if next[i].Type != model.ProjectStageTypeOutboundWebhook {
			continue
		}
		var cfg model.OutboundWebhookStageConfig
		if err := parseStageConfig(next[i], &cfg); err != nil {
			return fmt.Errorf("deploy_stages[%d].config is invalid: %v", i, err)
		}
		if !strings.Contains(cfg.URL, "******") {
			continue
		}
		oldCfg, ok := findExistingOutboundWebhookConfig(existing, next[i], i, used)
		if !ok || strings.TrimSpace(oldCfg.URL) == "" {
			continue
		}
		cfg.URL = oldCfg.URL
		raw, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("deploy_stages[%d].config is invalid: %v", i, err)
		}
		next[i].Config = raw
	}
	return nil
}

func findExistingOutboundWebhookConfig(stages []model.ProjectStage, target model.ProjectStage, index int, used map[int]bool) (model.OutboundWebhookStageConfig, bool) {
	for i, stage := range stages {
		if used[i] || stage.Type != model.ProjectStageTypeOutboundWebhook || stage.Name != target.Name {
			continue
		}
		cfg, ok := outboundWebhookConfig(stage)
		if ok {
			used[i] = true
			return cfg, true
		}
	}
	if index >= 0 && index < len(stages) && !used[index] {
		stage := stages[index]
		if stage.Type == model.ProjectStageTypeOutboundWebhook {
			cfg, ok := outboundWebhookConfig(stage)
			if ok {
				used[index] = true
				return cfg, true
			}
		}
	}
	return model.OutboundWebhookStageConfig{}, false
}

func outboundWebhookConfig(stage model.ProjectStage) (model.OutboundWebhookStageConfig, bool) {
	var cfg model.OutboundWebhookStageConfig
	if err := parseStageConfig(stage, &cfg); err != nil {
		return cfg, false
	}
	return cfg, true
}

func isMaskedValue(value interface{}) bool {
	s, ok := value.(string)
	return ok && strings.Contains(s, "******")
}
