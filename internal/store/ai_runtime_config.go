package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"texas_yu/internal/ai"
)

const defaultAIRuntimeConfigPath = "data/ai_runtime.json"

type AIRuntimeSettings struct {
	UseLLM bool   `json:"useLlm"`
	Model  string `json:"model"`
}

type AIRuntimeStatus struct {
	ConfigPath        string `json:"configPath"`
	UseLLM            bool   `json:"useLlm"`
	Model             string `json:"model"`
	DecisionMode      string `json:"decisionMode"`
	LLMConfigured     bool   `json:"llmConfigured"`
	APIKeyConfigured  bool   `json:"apiKeyConfigured"`
	BaseURL           string `json:"baseUrl"`
	APIFormat         string `json:"apiFormat"`
	TimeoutMs         int64  `json:"timeoutMs"`
	MaxRetry          int    `json:"maxRetry"`
	LastUpdatedAtUnix int64  `json:"lastUpdatedAtUnix"`
}

type aiRuntimeConfigFile struct {
	Version   int               `json:"version"`
	UpdatedAt string            `json:"updatedAt"`
	Settings  AIRuntimeSettings `json:"settings"`
}

func aiRuntimeConfigPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultAIRuntimeConfigPath
	}
	return trimmed
}

func defaultAIRuntimeSettings(cfg ai.Config, svc ai.Service) AIRuntimeSettings {
	model := strings.TrimSpace(cfg.Model)
	useLLM := false
	if cfg.Enabled() {
		useLLM = true
	} else if svc != nil && svc.Enabled() {
		useLLM = true
	}
	return AIRuntimeSettings{UseLLM: useLLM, Model: model}
}

func loadAIRuntimeSettingsFromFile(path string, defaults AIRuntimeSettings) (AIRuntimeSettings, bool, error) {
	cleanPath := aiRuntimeConfigPath(path)
	buf, err := os.ReadFile(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaults, false, nil
		}
		return defaults, false, err
	}
	var file aiRuntimeConfigFile
	if err := json.Unmarshal(buf, &file); err != nil {
		return defaults, false, err
	}
	settings := defaults
	settings.UseLLM = file.Settings.UseLLM
	if strings.TrimSpace(file.Settings.Model) != "" {
		settings.Model = strings.TrimSpace(file.Settings.Model)
	}
	return settings, true, nil
}

func saveAIRuntimeSettingsToFile(path string, settings AIRuntimeSettings) error {
	cleanPath := aiRuntimeConfigPath(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return err
	}
	payload := aiRuntimeConfigFile{
		Version:   1,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Settings:  AIRuntimeSettings{UseLLM: settings.UseLLM, Model: strings.TrimSpace(settings.Model)},
	}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cleanPath, append(buf, '\n'), 0o644)
}
