package store

import (
	"sort"
	"strings"

	"texas_yu/internal/ai"
)

func buildOpponentRangeHints(input ai.DecisionInput) []ai.OpponentRangeHint {
	actionSummary := summarizeVisibleActionsByUser(input.RecentActionLog, input.Stage)
	hints := make([]ai.OpponentRangeHint, 0, len(input.Players))
	for _, player := range input.Players {
		if player.UserID == input.AIUserID || player.Folded {
			continue
		}
		summary := actionSummary[player.UserID]
		stats := input.OpponentStats[player.UserID]
		profile := input.Profiles[player.UserID]
		tightBias, looseBias, aggressionBias, trapBias := profileRangeBias(profile)
		foldToPressure := clampFloat(0.38+float64(summary.CurrentStageChecks)*0.06+tightBias*0.28-stats.FoldRate*0.10-looseBias*0.18, 0.08, 0.88)
		if stats.Hands >= 3 {
			foldToPressure = clampFloat(foldToPressure+stats.FoldRate*0.40-stats.VPIP*0.08-stats.PFR*0.06, 0.05, 0.90)
		}
		trapRisk := clampFloat(0.08+trapBias*0.65+aggressionBias*0.10+float64(summary.CurrentStageChecks)*0.04, 0.02, 0.82)
		if stats.Hands >= 3 {
			trapRisk = clampFloat(trapRisk+stats.PFR*0.14+clampFloat((stats.AggressionFactor-1.8)*0.04, 0, 0.12), 0.02, 0.88)
		}
		drawWeight := clampFloat(0.16+boardWetness(input.CommunityCards)*0.42+float64(summary.CurrentStageCalls)*0.06+looseBias*0.10, 0.04, 0.86)
		preflopBucket := classifyPreflopBucket(summary, stats, tightBias, looseBias, aggressionBias)
		currentLine := classifyCurrentLine(player.LastAction, summary)
		likelyHandClass := classifyLikelyHandClass(input, player, summary, stats, foldToPressure, trapRisk, drawWeight)
		confidence := clampFloat(0.22+float64(stats.Hands)*0.05+float64(summary.PreflopCalls+summary.PreflopRaises+summary.CurrentStageCalls+summary.CurrentStageAgg+summary.CurrentStageChecks)*0.05, 0.18, 0.92)
		notes := []string{preflopBucket, currentLine, likelyHandClass}
		if foldToPressure >= 0.58 {
			notes = append(notes, "fold_prone")
		}
		if trapRisk >= 0.42 {
			notes = append(notes, "trap_risk")
		}
		if drawWeight >= 0.42 {
			notes = append(notes, "draw_heavy")
		}
		hints = append(hints, ai.OpponentRangeHint{
			UserID:          player.UserID,
			Username:        player.Username,
			PreflopBucket:   preflopBucket,
			CurrentLine:     currentLine,
			LikelyHandClass: likelyHandClass,
			FoldToPressure:  roundOptionMetric(foldToPressure),
			TrapRisk:        roundOptionMetric(trapRisk),
			DrawWeight:      roundOptionMetric(drawWeight),
			Confidence:      roundOptionMetric(confidence),
			Notes:           uniqueSortedNotes(notes),
		})
	}
	sort.Slice(hints, func(i, j int) bool {
		if hints[i].Confidence == hints[j].Confidence {
			return hints[i].UserID < hints[j].UserID
		}
		return hints[i].Confidence > hints[j].Confidence
	})
	return hints
}

func classifyPreflopBucket(summary visibleActionSummary, stats ai.OpponentStats, tightBias float64, looseBias float64, aggressionBias float64) string {
	switch {
	case summary.PreflopRaises > 0 && (aggressionBias >= 0.14 || stats.PFR >= 0.24):
		if tightBias >= 0.14 || stats.VPIP <= 0.24 {
			return "tight_strong_raise"
		}
		return "polarized_raise"
	case summary.PreflopCalls > 0:
		if looseBias >= 0.14 || stats.VPIP >= 0.36 {
			return "wide_call"
		}
		return "condensed_call"
	case tightBias >= 0.14 || stats.VPIP <= 0.22:
		return "tight_cap"
	default:
		return "unknown"
	}
}

func classifyCurrentLine(lastAction string, summary visibleActionSummary) string {
	lastAction = strings.ToLower(strings.TrimSpace(lastAction))
	switch {
	case summary.CurrentStageAgg > 0 || lastAction == "bet" || lastAction == "allin":
		return "aggressive_pressure"
	case summary.CurrentStageCalls > 0 || lastAction == "call":
		return "call_down"
	case summary.CurrentStageChecks > 0 || lastAction == "check":
		return "check_cap"
	default:
		return "unclear"
	}
}

func classifyLikelyHandClass(input ai.DecisionInput, player ai.PlayerSnapshot, summary visibleActionSummary, stats ai.OpponentStats, foldToPressure float64, trapRisk float64, drawWeight float64) string {
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	aggressive := summary.CurrentStageAgg > 0 || strings.EqualFold(player.LastAction, "bet") || strings.EqualFold(player.LastAction, "allin")
	calling := summary.CurrentStageCalls > 0 || strings.EqualFold(player.LastAction, "call")
	switch {
	case aggressive && (trapRisk >= 0.34 || stats.PFR >= 0.24):
		if stage == "river" && drawWeight < 0.24 {
			return "polarized_value_bluff"
		}
		return "strong_made_or_draw"
	case calling && drawWeight >= 0.42:
		return "pair_plus_draw"
	case calling && foldToPressure <= 0.34:
		return "sticky_showdown"
	case foldToPressure >= 0.60:
		return "capped_marginal"
	case stage == "river" && drawWeight >= 0.36:
		return "missed_draw_heavy"
	default:
		return "mixed_medium_strength"
	}
}
