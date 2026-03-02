package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type NoopService struct{}

func (NoopService) Enabled() bool { return false }

func (NoopService) DecideAction(_ context.Context, _ DecisionInput) (Decision, error) {
	return Decision{}, errors.New("ai disabled")
}

func (NoopService) SummarizeHand(_ context.Context, _ SummaryInput) (Summary, error) {
	return Summary{}, errors.New("ai disabled")
}

type OpenAIService struct {
	cfg    Config
	client *openAIClient
}

func NewService(cfg Config) Service {
	if !cfg.Enabled() {
		return NoopService{}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if cfg.MaxRetry < 0 {
		cfg.MaxRetry = 0
	}
	return &OpenAIService{cfg: cfg, client: newOpenAIClient(cfg)}
}

func (s *OpenAIService) Enabled() bool { return true }

func (s *OpenAIService) DecideAction(ctx context.Context, input DecisionInput) (Decision, error) {
	systemPrompt, userPrompt := BuildDecisionPrompt(input)
	var lastErr error
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		tCtx, cancel := withTimeout(ctx, s.cfg.Timeout)
		raw, err := s.client.runJSON(tCtx, systemPrompt, userPrompt)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		decision, err := decodeDecision(raw)
		if err != nil {
			lastErr = err
			continue
		}
		if err := validateDecision(input, decision); err != nil {
			lastErr = err
			continue
		}
		return decision, nil
	}
	if lastErr == nil {
		lastErr = errors.New("failed to get ai decision")
	}
	return Decision{}, lastErr
}

func (s *OpenAIService) SummarizeHand(ctx context.Context, input SummaryInput) (Summary, error) {
	systemPrompt, userPrompt := BuildSummaryPrompt(input)
	var lastErr error
	for attempt := 0; attempt <= s.cfg.MaxRetry; attempt++ {
		tCtx, cancel := withTimeout(ctx, s.cfg.Timeout)
		raw, err := s.client.runJSON(tCtx, systemPrompt, userPrompt)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		summary, err := decodeSummary(raw)
		if err != nil {
			lastErr = err
			continue
		}
		if err := validateSummary(summary); err != nil {
			lastErr = err
			continue
		}
		return summary, nil
	}
	if lastErr == nil {
		lastErr = errors.New("failed to summarize hand")
	}
	return Summary{}, lastErr
}

func decodeDecision(raw string) (Decision, error) {
	var out Decision
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Decision{}, err
	}
	out.Action = strings.TrimSpace(strings.ToLower(out.Action))
	if out.Amount < 0 {
		out.Amount = 0
	}
	return out, nil
}

func validateDecision(input DecisionInput, d Decision) error {
	if d.Action == "" {
		return errors.New("empty action")
	}
	allowed := map[string]bool{}
	for _, item := range input.AllowedActions {
		allowed[strings.ToLower(strings.TrimSpace(item))] = true
	}
	if !allowed[d.Action] {
		return fmt.Errorf("action %s not allowed", d.Action)
	}
	switch d.Action {
	case "check", "call", "allin", "fold":
		if d.Action != "allin" {
			d.Amount = 0
		}
		return nil
	case "bet":
		min := input.MinBet
		if input.RoundBet > 0 {
			min = input.MinRaise
		}
		if d.Amount < min || d.Amount > input.Stack {
			return fmt.Errorf("bet amount out of range")
		}
		return nil
	default:
		return fmt.Errorf("unsupported action %s", d.Action)
	}
}

func decodeSummary(raw string) (Summary, error) {
	var out Summary
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Summary{}, err
	}
	out.HandSummary = strings.TrimSpace(out.HandSummary)
	if out.OpponentProfiles == nil {
		out.OpponentProfiles = map[string]Profile{}
	}
	for uid, profile := range out.OpponentProfiles {
		profile.Style = strings.TrimSpace(profile.Style)
		profile.Advice = strings.TrimSpace(profile.Advice)
		if profile.Tendencies == nil {
			profile.Tendencies = []string{}
		}
		for i := range profile.Tendencies {
			profile.Tendencies[i] = strings.TrimSpace(profile.Tendencies[i])
		}
		out.OpponentProfiles[uid] = profile
	}
	return out, nil
}

func validateSummary(s Summary) error {
	if s.HandSummary == "" {
		return errors.New("empty hand summary")
	}
	for userID, profile := range s.OpponentProfiles {
		if strings.TrimSpace(userID) == "" {
			return errors.New("empty profile key")
		}
		if profile.Style == "" {
			return fmt.Errorf("profile style empty for %s", userID)
		}
	}
	return nil
}
