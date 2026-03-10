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
	stream := c.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(c.cfg.Model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	})
	if stream == nil {
		return "", fmt.Errorf("chat completion stream unavailable")
	}
	defer stream.Close()

	var out strings.Builder
	var refusal strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			if choice.Delta.Content != "" {
				out.WriteString(choice.Delta.Content)
			}
			if choice.Delta.Refusal != "" {
				refusal.WriteString(choice.Delta.Refusal)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		if refusal.Len() > 0 {
			return "", fmt.Errorf("chat completion refused: %s", strings.TrimSpace(refusal.String()))
		}
		return "", fmt.Errorf("empty chat choices")
	}
	return extractJSONObject(out.String()), nil
}

func (c *openAIClient) runResponses(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	stream := c.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{
		Model:        openai.ChatModel(c.cfg.Model),
		Instructions: openai.String(systemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(userPrompt),
		},
	})
	if stream == nil {
		return "", fmt.Errorf("response stream unavailable")
	}
	defer stream.Close()

	var out strings.Builder
	var refusal strings.Builder
	sawDeltaByPart := map[string]bool{}
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta()
			if delta.Delta != "" {
				out.WriteString(delta.Delta)
				key := fmt.Sprintf("%d:%d", delta.OutputIndex, delta.ContentIndex)
				sawDeltaByPart[key] = true
			}
		case "response.output_text.done":
			done := event.AsResponseOutputTextDone()
			if done.Text == "" {
				break
			}
			key := fmt.Sprintf("%d:%d", done.OutputIndex, done.ContentIndex)
			if !sawDeltaByPart[key] {
				out.WriteString(done.Text)
			}
		case "response.refusal.delta":
			delta := event.AsResponseRefusalDelta()
			if delta.Delta != "" {
				refusal.WriteString(delta.Delta)
			}
		case "response.refusal.done":
			if refusal.Len() == 0 {
				done := event.AsResponseRefusalDone()
				if done.Refusal != "" {
					refusal.WriteString(done.Refusal)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		if refusal.Len() > 0 {
			return "", fmt.Errorf("response refused: %s", strings.TrimSpace(refusal.String()))
		}
		return "", fmt.Errorf("empty response output")
	}
	return extractJSONObject(out.String()), nil
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
