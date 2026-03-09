package store

import (
	"strings"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
)

func shouldPreferFallbackDecision(input ai.DecisionInput, decision ai.Decision, fallback ai.Decision, equity float64) bool {
	if !decisionAllowedByInput(input, fallback) || !decisionPassesEVGuard(input, fallback, equity) {
		return false
	}
	modelAction := strings.ToLower(strings.TrimSpace(decision.Action))
	fallbackAction := strings.ToLower(strings.TrimSpace(fallback.Action))
	if modelAction == "" || fallbackAction == "" {
		return false
	}

	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	facingBet := input.CallAmount > 0
	potOdds := callPotOdds(input)
	effectiveEquity := clampFloat(equity+drawEquityBonus(input), 0, 1)
	opponents := activeOpponentCount(input)
	stationScore := activeOpponentCallStationScore(input)
	initiative := heroHasInitiative(input)
	lineCapScore := visibleRangeCapScore(input)
	pressure := estimateFallbackPressure(input)
	strongDraw, _ := hasDrawPotential(input.DrawFlags)
	shortStack := float64(input.Stack)/float64(maxInt(1, input.Pot)) <= 1.35 || input.Stack <= maxInt(input.OpenBetMin*7, input.CallAmount+input.BetMin*2)

	heroHole, heroBoard, cardsOK := parseDecisionCards(input)
	heroCategoryRank := input.HandCategoryRank
	pairScore := 0.0
	blockerScore := 0.0
	missedDrawScore := 0.0
	showdownValueScore := 0.0
	scareScore := 0.0
	if cardsOK {
		if len(heroHole)+len(heroBoard) >= 5 {
			best, _, _ := domain.BestOfSeven(append(append([]domain.Card{}, heroBoard...), heroHole...))
			heroCategoryRank = best.Category
		}
		pairScore = pairStrengthScore(heroHole, heroBoard)
		scareScore = latestBoardScareScore(heroBoard)
		if stage == "river" {
			blockerScore = riverHeroBlockerScore(heroHole, heroBoard, heroCategoryRank)
			missedDrawScore = riverMissedDrawScore(heroHole, heroBoard, heroCategoryRank)
			showdownValueScore = riverShowdownValueScore(heroHole, heroBoard, heroCategoryRank, pairScore)
		}
	}

	if modelAction == fallbackAction {
		if modelAction == "bet" {
			pot := maxInt(1, input.Pot)
			modelRatio := float64(decision.Amount) / float64(pot)
			fallbackRatio := float64(fallback.Amount) / float64(pot)
			if stage == "river" && heroCategoryRank <= 1 && stationScore >= 0.10 && modelRatio > fallbackRatio+0.45 {
				return true
			}
			if !facingBet && opponents >= 2 && effectiveEquity < 0.35 && !strongDraw && modelRatio > 0.95 {
				return true
			}
		}
		return false
	}

	switch {
	case stage == "river" && facingBet && modelAction == "call" && fallbackAction == "fold":
		expensiveCall := input.CallAmount > maxInt(1, input.Pot/2)
		weakBluffCatcher := heroCategoryRank <= 1 && pairScore < 0.56 && blockerScore < 0.18
		thinShowdown := showdownValueScore < 0.56
		if effectiveEquity+0.02 < potOdds || (weakBluffCatcher && thinShowdown && (stationScore >= 0.10 || expensiveCall)) {
			return true
		}
	case stage == "river" && !facingBet && modelAction == "bet" && fallbackAction == "check":
		weakThinValue := heroCategoryRank <= 1 && pairScore < 0.58 && stationScore >= 0.10
		purePuntBluff := heroCategoryRank <= 1 && blockerScore < 0.18 && missedDrawScore < 0.18 && showdownValueScore >= 0.24
		multiwayPunt := opponents >= 2 && heroCategoryRank <= 1 && blockerScore < 0.24
		if weakThinValue || purePuntBluff || multiwayPunt {
			return true
		}
	case facingBet && modelAction == "fold" && (fallbackAction == "call" || fallbackAction == "allin"):
		clearContinue := effectiveEquity > potOdds+0.06
		if stage == "river" && (heroCategoryRank >= 2 || pairScore >= 0.64) && input.CallAmount <= maxInt(1, input.Pot/3) {
			clearContinue = true
		}
		if clearContinue {
			return true
		}
	case !facingBet && modelAction == "check" && fallbackAction == "bet":
		strongValue := heroCategoryRank >= 2 || pairScore >= 0.68
		goodPressureBet := stage != "preflop" && initiative && lineCapScore >= 0.10 && (strongDraw || effectiveEquity >= 0.62 || scareScore >= 0.10)
		if strongValue || goodPressureBet {
			return true
		}
	case modelAction == "allin" && fallbackAction != "allin":
		jamRoll := deterministicRoll(input, "jam-guard-model")
		if !shouldFallbackJam(input, equity, pressure, opponents, jamRoll) && !shortStack {
			return true
		}
	}

	return false
}
