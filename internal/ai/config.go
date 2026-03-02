package ai

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type APIFormat string

const (
	APIFormatChatCompletions APIFormat = "chat_completions"
	APIFormatResponses       APIFormat = "responses"
)

type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	APIFormat APIFormat
	Timeout   time.Duration
	MaxRetry  int
}

func LoadConfigFromEnv() Config {
	timeoutMs := parseIntWithDefault(strings.TrimSpace(os.Getenv("AI_TIMEOUT_MS")), 8000)
	if timeoutMs <= 0 {
		timeoutMs = 8000
	}
	maxRetry := parseIntWithDefault(strings.TrimSpace(os.Getenv("AI_MAX_RETRY")), 2)
	if maxRetry < 0 {
		maxRetry = 0
	}
	formatRaw := strings.TrimSpace(strings.ToLower(os.Getenv("AI_API_FORMAT")))
	format := APIFormatChatCompletions
	if formatRaw == string(APIFormatResponses) {
		format = APIFormatResponses
	}
	return Config{
		BaseURL:   strings.TrimSpace(os.Getenv("AI_BASE_URL")),
		APIKey:    strings.TrimSpace(os.Getenv("AI_API_KEY")),
		Model:     strings.TrimSpace(os.Getenv("AI_MODEL")),
		APIFormat: format,
		Timeout:   time.Duration(timeoutMs) * time.Millisecond,
		MaxRetry:  maxRetry,
	}
}

func (c Config) Enabled() bool {
	return c.APIKey != "" && c.Model != ""
}

func parseIntWithDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
