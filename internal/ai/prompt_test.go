package ai

import (
	"strings"
	"testing"
)

func TestBuildDecisionPrompt_IncludesDiagnosticsAndBaseline(t *testing.T) {
	baseline := &Decision{Action: "call", Amount: 0}
	input := DecisionInput{
		AllowedActions:  []string{"call", "fold"},
		HoleCards:       []string{"AS", "KD"},
		CommunityCards:  []string{"2C", "7D", "9H"},
		DecisionOptions: []DecisionOption{{ID: "call", Action: "call", Amount: 0, EVEstimate: 14.2, LocalScore: 0.72, RiskScore: 0.14}},
		OpponentRanges:  []OpponentRangeHint{{UserID: "p-1", PreflopBucket: "wide_call", CurrentLine: "check_cap", LikelyHandClass: "capped_marginal", FoldToPressure: 0.64, Confidence: 0.71}},
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
	if !strings.Contains(user, "decisionOptions") {
		t.Fatalf("expected prompt to include decisionOptions")
	}
	if !strings.Contains(user, "optionId") {
		t.Fatalf("expected prompt to include optionId")
	}
	if !strings.Contains(user, "opponentRanges") {
		t.Fatalf("expected prompt to include opponentRanges")
	}
	if !strings.Contains(user, "evEstimate") {
		t.Fatalf("expected prompt to include evEstimate")
	}
}
