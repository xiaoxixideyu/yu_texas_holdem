package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
)

type openAIClient struct {
	cfg    Config
	client openai.Client
}

func newOpenAIClient(cfg Config) *openAIClient {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	return &openAIClient{cfg: cfg, client: openai.NewClient(opts...)}
}

func (c *openAIClient) runJSON(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.cfg.APIFormat == APIFormatResponses {
		return c.runResponses(ctx, systemPrompt, userPrompt)
	}
	return c.runChatCompletions(ctx, systemPrompt, userPrompt)
}

func (c *openAIClient) runChatCompletions(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(c.cfg.Model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty chat choices")
	}
	return extractJSONObject(resp.Choices[0].Message.Content), nil
}

func (c *openAIClient) runResponses(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model:        openai.ChatModel(c.cfg.Model),
		Instructions: openai.String(systemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
	})
	if err != nil {
		return "", err
	}
	return extractJSONObject(resp.OutputText()), nil
}

func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = 8 * time.Second
	}
	return context.WithTimeout(parent, d)
}

func mustJSON(v any) string {
	buf, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(buf)
}

func extractJSONObject(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}
