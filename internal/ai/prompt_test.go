package ai

import (
	"strings"
	"testing"
)

func TestBuildDecisionPrompt_IncludesDiagnosticsAndBaseline(t *testing.T) {
	baseline := &Decision{Action: "call", Amount: 0}
	input := DecisionInput{
		AllowedActions: []string{"call", "fold"},
		HoleCards:      []string{"AS", "KD"},
		CommunityCards: []string{"2C", "7D", "9H"},
		Diagnostics: DecisionDiagnostics{
			EquityEstimate: 0.61,
			PotOdds:        0.25,
			VisibleTags:    []string{"heads_up", "initiative"},
		},
		BaselineDecision: baseline,
	}
	_, user := BuildDecisionPrompt(input)
	if !strings.Contains(user, "baselineDecision") {
		t.Fatalf("expected prompt to include baselineDecision")
	}
	if !strings.Contains(user, "diagnostics") {
		t.Fatalf("expected prompt to include diagnostics")
	}
	if !strings.Contains(user, "visibleTags") {
		t.Fatalf("expected prompt to include visibleTags")
	}
}
