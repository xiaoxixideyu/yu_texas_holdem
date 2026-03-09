package store

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
)

type decisionOptionContext struct {
	stage              string
	facingBet          bool
	equity             float64
	equityWithDraw     float64
	potOdds            float64
	pressure           float64
	wetness            float64
	opponents          int
	stationScore       float64
	initiative         bool
	rangeAdv           float64
	lineCapScore       float64
	barrelCount        int
	previousBarrelCall bool
	strongDraw         bool
	weakDraw           bool
	shortStack         bool
	heroCategoryRank   int
	pairScore          float64
	blockerScore       float64
	missedDrawScore    float64
	showdownValueScore float64
	scareScore         float64
}

func buildDecisionOptionContext(input ai.DecisionInput) decisionOptionContext {
	ctx := decisionOptionContext{
		stage:              strings.ToLower(strings.TrimSpace(input.Stage)),
		facingBet:          input.CallAmount > 0,
		equity:             estimateFallbackEquity(input),
		potOdds:            callPotOdds(input),
		pressure:           estimateFallbackPressure(input),
		wetness:            boardWetness(input.CommunityCards),
		opponents:          activeOpponentCount(input),
		stationScore:       activeOpponentCallStationScore(input),
		initiative:         heroHasInitiative(input),
		lineCapScore:       visibleRangeCapScore(input),
		barrelCount:        heroPostflopBarrelCount(input),
		previousBarrelCall: previousStreetHeroBarrelCalled(input),
	}
	ctx.strongDraw, ctx.weakDraw = hasDrawPotential(input.DrawFlags)
	ctx.equityWithDraw = clampFloat(ctx.equity+drawEquityBonus(input), 0, 1)
	ctx.shortStack = float64(input.Stack)/float64(maxInt(1, input.Pot)) <= 1.35 || input.Stack <= maxInt(input.OpenBetMin*7, input.CallAmount+input.BetMin*2)

	heroHole, heroBoard, cardsOK := parseDecisionCards(input)
	ctx.heroCategoryRank = input.HandCategoryRank
	if cardsOK {
		if len(heroHole)+len(heroBoard) >= 5 {
			best, _, _ := domain.BestOfSeven(append(append([]domain.Card{}, heroBoard...), heroHole...))
			ctx.heroCategoryRank = best.Category
		}
		ctx.pairScore = pairStrengthScore(heroHole, heroBoard)
		ctx.rangeAdv = boardRangeAdvantage(input, heroBoard)
		ctx.scareScore = latestBoardScareScore(heroBoard)
		if ctx.stage == "river" {
			ctx.blockerScore = riverHeroBlockerScore(heroHole, heroBoard, ctx.heroCategoryRank)
			ctx.missedDrawScore = riverMissedDrawScore(heroHole, heroBoard, ctx.heroCategoryRank)
			ctx.showdownValueScore = riverShowdownValueScore(heroHole, heroBoard, ctx.heroCategoryRank, ctx.pairScore)
		}
	}
	return ctx
}

func buildDecisionOptions(input ai.DecisionInput, params StrategyParams, baseline ai.Decision) []ai.DecisionOption {
	ctx := buildDecisionOptionContext(input)
	allowed := map[string]bool{}
	for _, action := range input.AllowedActions {
		allowed[strings.ToLower(strings.TrimSpace(action))] = true
	}
	betMin := input.MinBet
	if input.RoundBet > 0 {
		betMin = input.MinRaise
	}
	if betMin <= 0 {
		betMin = 1
	}
	canBet := allowed["bet"] && input.Stack >= betMin
	profileFoldAdj, profileValueAdj, _ := profileStrategyAdjustments(input.Profiles)
	statsFoldAdj, statsValueAdj, _ := opponentStatsStrategyAdjustments(input.OpponentStats)
	foldEqAdj := clampFloat(profileFoldAdj+statsFoldAdj, -0.22, 0.22)
	valueAdj := clampFloat(profileValueAdj+statsValueAdj, -0.10, 0.26)

	ctxForBet := fallbackBetSizingContext{
		Initiative:      ctx.initiative,
		RangeAdv:        ctx.rangeAdv,
		ScareScore:      ctx.scareScore,
		BlockerScore:    ctx.blockerScore,
		MissedDrawScore: ctx.missedDrawScore,
		CappedScore:     ctx.lineCapScore,
		BarrelCount:     ctx.barrelCount,
	}

	type optionEntry struct {
		option ai.DecisionOption
	}
	entries := map[string]optionEntry{}
	mergeOption := func(id string, mode string, decision ai.Decision, isBaseline bool, extraNotes ...string) {
		decision = materializeDecisionOption(ai.DecisionInput{DecisionOptions: input.DecisionOptions}, decision)
		if !decisionAllowedByInput(input, decision) {
			return
		}
		if !isBaseline && !decisionPassesEVGuard(input, decision, ctx.equity) {
			return
		}
		score, risk, notes := scoreDecisionOption(input, ctx, decision)
		evEstimate := approximateDecisionEV(input, ctx, decision)
		notes = append(notes, extraNotes...)
		if isBaseline {
			notes = append(notes, "baseline")
		}
		notes = uniqueSortedNotes(notes)
		key := fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(decision.Action)), decision.Amount)
		opt := ai.DecisionOption{
			ID:         strings.ToLower(strings.TrimSpace(id)),
			Action:     strings.ToLower(strings.TrimSpace(decision.Action)),
			Amount:     decision.Amount,
			Mode:       mode,
			EVEstimate: roundOptionMetric(evEstimate),
			LocalScore: roundOptionMetric(score),
			RiskScore:  roundOptionMetric(risk),
			IsBaseline: isBaseline,
			Notes:      notes,
		}
		if current, ok := entries[key]; ok {
			current.option.Notes = uniqueSortedNotes(append(current.option.Notes, opt.Notes...))
			current.option.IsBaseline = current.option.IsBaseline || opt.IsBaseline
			if current.option.IsBaseline {
				current.option.ID = "baseline"
				if current.option.Mode == "" {
					current.option.Mode = mode
				}
			}
			if opt.LocalScore > current.option.LocalScore {
				current.option.EVEstimate = opt.EVEstimate
				current.option.LocalScore = opt.LocalScore
				current.option.RiskScore = opt.RiskScore
				if !current.option.IsBaseline {
					current.option.ID = opt.ID
					current.option.Mode = opt.Mode
				}
			}
			entries[key] = current
			return
		}
		entries[key] = optionEntry{option: opt}
	}

	mergeOption("baseline", "baseline", baseline, true)
	if ctx.facingBet {
		if allowed["call"] {
			mergeOption("call", "continue", ai.Decision{Action: "call", Amount: 0}, false)
		}
		if allowed["fold"] {
			mergeOption("fold", "defend", ai.Decision{Action: "fold", Amount: 0}, false)
		}
	} else if allowed["check"] {
		mergeOption("check", "control", ai.Decision{Action: "check", Amount: 0}, false)
	}

	if allowed["allin"] {
		jamRoll := deterministicRoll(input, "option-jam")
		if shouldFallbackJam(input, ctx.equity, ctx.pressure, ctx.opponents, jamRoll) || ctx.shortStack || (ctx.strongDraw && ctx.shortStack) || ctx.equity >= 0.82 {
			mergeOption("allin", "jam", ai.Decision{Action: "allin", Amount: 0}, false)
		}
	}

	if canBet {
		modeSet := map[string]bool{}
		addMode := func(mode string) {
			if mode != "" {
				modeSet[mode] = true
			}
		}
		addMode("probe")
		if ctx.strongDraw {
			addMode("semi_bluff")
		}
		if ctx.heroCategoryRank >= 2 || ctx.pairScore >= 0.60 || ctx.equity >= 0.70 || valueAdj >= 0.08 {
			addMode("value")
		}
		if ctx.facingBet && (ctx.equity >= 0.64 || ctx.strongDraw) {
			addMode("semi_bluff")
			if ctx.equity >= 0.74 {
				addMode("value")
			}
		}
		if ctx.stage == "preflop" {
			if input.PreflopFacingRaise {
				if ctx.equity >= 0.58 || valueAdj >= 0.06 {
					addMode("value")
				}
				if foldEqAdj >= 0.05 && input.PreflopTier != "trash" {
					addMode("bluff")
				}
			} else {
				position := strings.ToLower(strings.TrimSpace(input.PreflopPosition))
				if position == "btn" || position == "co" || position == "btn_sb" {
					addMode("bluff")
				}
			}
		} else {
			if ctx.lineCapScore >= 0.08 || ctx.rangeAdv >= 0.08 || ctx.initiative {
				addMode("probe")
			}
			if foldEqAdj >= 0.05 || ctx.lineCapScore >= 0.10 || ctx.scareScore >= 0.10 {
				addMode("bluff")
			}
			if ctx.stage == "turn" && ctx.initiative && (ctx.scareScore >= 0.10 || ctx.previousBarrelCall || ctx.lineCapScore >= 0.10) {
				addMode("polarize")
			}
			if ctx.stage == "river" && ctx.heroCategoryRank <= 1 && (ctx.blockerScore >= 0.18 || ctx.missedDrawScore >= 0.18 || ctx.lineCapScore >= 0.10) {
				addMode("bluff")
				addMode("polarize")
			}
		}
		orderedModes := []string{"value", "probe", "semi_bluff", "bluff", "polarize"}
		for _, mode := range orderedModes {
			if !modeSet[mode] {
				continue
			}
			amount := chooseFallbackBetAmount(input, betMin, mode, deterministicRoll(input, "option-"+mode), ctx.wetness, ctx.pressure, ctxForBet, params)
			mergeOption("bet_"+mode, mode, ai.Decision{Action: "bet", Amount: clampInt(amount, betMin, input.Stack)}, false)
		}
	}

	options := make([]ai.DecisionOption, 0, len(entries))
	baselineKey := fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(baseline.Action)), baseline.Amount)
	baselineOption, hasBaseline := entries[baselineKey]
	for _, entry := range entries {
		options = append(options, entry.option)
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].EVEstimate != options[j].EVEstimate {
			return options[i].EVEstimate > options[j].EVEstimate
		}
		if options[i].LocalScore == options[j].LocalScore {
			if options[i].IsBaseline != options[j].IsBaseline {
				return options[i].IsBaseline
			}
			if options[i].RiskScore == options[j].RiskScore {
				return options[i].ID < options[j].ID
			}
			return options[i].RiskScore < options[j].RiskScore
		}
		return options[i].LocalScore > options[j].LocalScore
	})
	if len(options) > 6 {
		trimmed := make([]ai.DecisionOption, 0, 6)
		baselineIncluded := false
		for _, option := range options {
			if len(trimmed) < 6 {
				trimmed = append(trimmed, option)
				baselineIncluded = baselineIncluded || option.IsBaseline
				continue
			}
			if option.IsBaseline && !baselineIncluded {
				trimmed[len(trimmed)-1] = option
				baselineIncluded = true
			}
		}
		options = trimmed
	}
	if hasBaseline {
		baselineIncluded := false
		for _, option := range options {
			if option.IsBaseline {
				baselineIncluded = true
				break
			}
		}
		if !baselineIncluded {
			options = append(options, baselineOption.option)
		}
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].EVEstimate != options[j].EVEstimate {
			return options[i].EVEstimate > options[j].EVEstimate
		}
		if options[i].LocalScore == options[j].LocalScore {
			if options[i].IsBaseline != options[j].IsBaseline {
				return options[i].IsBaseline
			}
			if options[i].RiskScore == options[j].RiskScore {
				return options[i].ID < options[j].ID
			}
			return options[i].RiskScore < options[j].RiskScore
		}
		return options[i].LocalScore > options[j].LocalScore
	})
	return options
}

func scoreDecisionOption(input ai.DecisionInput, ctx decisionOptionContext, decision ai.Decision) (float64, float64, []string) {
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	score := 0.20 + ctx.equity*0.70 - ctx.pressure*0.08 - float64(maxInt(0, ctx.opponents-1))*0.03
	risk := 0.12 + ctx.pressure*0.16
	notes := []string{}
	rangeFold, rangeTrap, rangeDraw := opponentRangeAverages(input.OpponentRanges)
	if ctx.facingBet {
		notes = append(notes, "facing_bet")
	}
	if ctx.initiative {
		notes = append(notes, "initiative")
	}
	if ctx.strongDraw {
		notes = append(notes, "strong_draw")
	} else if ctx.weakDraw {
		notes = append(notes, "weak_draw")
	}
	if ctx.lineCapScore >= 0.10 {
		notes = append(notes, "villain_capped")
	}
	if ctx.stationScore >= 0.10 {
		notes = append(notes, "station_risk")
	}
	if rangeFold >= 0.55 {
		notes = append(notes, "range_fold_pressure")
	}
	if rangeTrap >= 0.38 {
		notes = append(notes, "range_trap_risk")
	}
	if rangeDraw >= 0.40 {
		notes = append(notes, "range_draw_weight")
	}

	switch action {
	case "fold":
		if ctx.facingBet {
			edge := ctx.potOdds - ctx.equityWithDraw
			score = 0.42 + edge*1.35 + ctx.stationScore*0.08 + rangeTrap*0.10
			if ctx.equityWithDraw > ctx.potOdds+0.06 {
				score -= 0.30
			}
			if ctx.heroCategoryRank >= 2 || ctx.pairScore >= 0.64 {
				score -= 0.18
			}
		} else {
			score = 0.05
		}
		risk = 0.03
		notes = append(notes, "pot_control")
	case "check":
		score = 0.42 + ctx.showdownValueScore*0.42 + ctx.stationScore*0.10 - ctx.lineCapScore*0.10 + rangeTrap*0.06
		if ctx.heroCategoryRank >= 2 || ctx.pairScore >= 0.68 {
			score -= 0.18
		}
		if ctx.stage == "river" && ctx.showdownValueScore >= 0.28 {
			notes = append(notes, "showdown_value")
		}
		risk = 0.08
		notes = append(notes, "pot_control")
	case "call":
		edge := ctx.equityWithDraw - ctx.potOdds
		score = 0.48 + edge*1.55 - ctx.pressure*0.10 + ctx.showdownValueScore*0.10 + rangeDraw*0.08 - rangeTrap*0.04
		if ctx.stage == "river" && ctx.heroCategoryRank <= 1 {
			score += ctx.blockerScore * 0.12
		}
		if ctx.stage == "river" && ctx.showdownValueScore < 0.18 && ctx.blockerScore < 0.14 {
			score -= 0.08
		}
		risk = 0.26 + ctx.pressure*0.32
		notes = append(notes, "realize_equity")
	case "bet":
		ratio := float64(decision.Amount) / float64(maxInt(1, input.Pot))
		bluffEdge := ctx.lineCapScore*0.34 + ctx.blockerScore*0.22 + ctx.missedDrawScore*0.18 + ctx.scareScore*0.16 + ctx.rangeAdv*0.20 + rangeFold*0.22 - rangeTrap*0.14
		valueEdge := ctx.equity*0.55 + ctx.pairScore*0.26 + float64(maxInt(0, ctx.heroCategoryRank))*0.05
		score = 0.38 + valueEdge + ctx.lineCapScore*0.10 - ctx.stationScore*0.05 + rangeFold*0.06 - rangeTrap*0.05
		if ctx.heroCategoryRank <= 1 && ctx.pairScore < 0.58 {
			score = 0.34 + bluffEdge - ctx.showdownValueScore*0.24 - ctx.stationScore*0.18 - ctx.pressure*0.06
			if ctx.strongDraw {
				score += 0.12
			}
			notes = append(notes, "pressure")
		} else {
			notes = append(notes, "value")
		}
		if ctx.stage == "river" {
			if ctx.heroCategoryRank <= 1 {
				score += ctx.blockerScore*0.20 + ctx.missedDrawScore*0.18 - ctx.showdownValueScore*0.28
				if ctx.blockerScore >= 0.18 || ctx.missedDrawScore >= 0.18 {
					notes = append(notes, "blocker")
				}
			} else {
				score += ctx.lineCapScore*0.14 - ctx.stationScore*0.10
			}
		}
		if ctx.opponents >= 3 && ratio > 0.90 && ctx.equity < 0.38 && !ctx.strongDraw {
			score -= 0.22
		}
		if ratio > 1.05 && ctx.heroCategoryRank <= 1 && ctx.blockerScore < 0.20 && ctx.missedDrawScore < 0.20 {
			score -= 0.18
		}
		if ctx.stage == "flop" && ctx.initiative && ctx.wetness <= 0.42 && ratio <= 0.45 {
			score += 0.05
		}
		risk = 0.22 + ratio*0.28 + ctx.pressure*0.18
	case "allin":
		commit := float64(input.Stack) / float64(maxInt(1, input.Pot+input.Stack))
		jamRoll := deterministicRoll(input, "option-score-jam")
		score = 0.34 + ctx.equity*0.95 - commit*0.20 - ctx.stationScore*0.05 + rangeFold*0.10 - rangeTrap*0.08
		if shouldFallbackJam(input, ctx.equity, ctx.pressure, ctx.opponents, jamRoll) {
			score += 0.18
		}
		if !ctx.shortStack && ctx.equity < 0.68 && !ctx.strongDraw {
			score -= 0.20
		}
		risk = 0.82
		notes = append(notes, "high_leverage")
	}
	return clampFloat(score, 0.02, 1.35), clampFloat(risk, 0.02, 1.00), uniqueSortedNotes(notes)
}

func approximateDecisionEV(input ai.DecisionInput, ctx decisionOptionContext, decision ai.Decision) float64 {
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	pot := float64(maxInt(1, input.Pot))
	callAmount := float64(maxInt(0, input.CallAmount))
	invest := float64(maxInt(0, decision.Amount))
	if action == "allin" {
		invest = float64(maxInt(0, input.Stack))
	}
	_, rangeTrap, rangeDraw := opponentRangeAverages(input.OpponentRanges)
	switch action {
	case "fold":
		return 0
	case "check":
		return pot * (ctx.equity*0.18 + ctx.showdownValueScore*0.16 + ctx.rangeAdv*0.05 - rangeTrap*0.03)
	case "call":
		finalPot := pot + callAmount
		equity := clampFloat(ctx.equityWithDraw+rangeDraw*0.03-rangeTrap*0.02, 0.01, 0.99)
		return equity*finalPot - (1.0-equity)*callAmount
	case "bet":
		foldEq := estimateOptionFoldEquity(input, ctx, decision, input.OpponentRanges)
		equity := clampFloat(maxFloat(ctx.equity, ctx.equityWithDraw*0.94)-rangeTrap*0.02, 0.01, 0.99)
		calledPot := pot + invest + invest
		return foldEq*pot + (1.0-foldEq)*(equity*calledPot-(1.0-equity)*invest)
	case "allin":
		foldEq := estimateOptionFoldEquity(input, ctx, decision, input.OpponentRanges)
		equity := clampFloat(ctx.equityWithDraw+rangeDraw*0.02-rangeTrap*0.03, 0.01, 0.99)
		calledPot := pot + invest + invest
		return foldEq*pot + (1.0-foldEq)*(equity*calledPot-(1.0-equity)*invest)
	default:
		return pot * (ctx.equity*0.08 - 0.02)
	}
}

func estimateOptionFoldEquity(input ai.DecisionInput, ctx decisionOptionContext, decision ai.Decision, hints []ai.OpponentRangeHint) float64 {
	rangeFold, rangeTrap, _ := opponentRangeAverages(hints)
	ratio := 0.0
	if decision.Action == "bet" {
		ratio = float64(decision.Amount) / float64(maxInt(1, input.Pot))
	}
	base := 0.10 + ctx.lineCapScore*0.28 + ctx.scareScore*0.18 + rangeFold*0.56 - ctx.stationScore*0.34 - rangeTrap*0.24 - float64(maxInt(0, ctx.opponents-1))*0.07
	if decision.Action == "bet" {
		base += clampFloat(ratio-0.35, -0.10, 0.22)
	}
	if decision.Action == "allin" {
		if ctx.shortStack {
			base += 0.10
		} else {
			base -= 0.04
		}
	}
	if ctx.heroCategoryRank <= 1 && (ctx.blockerScore >= 0.18 || ctx.missedDrawScore >= 0.18) {
		base += 0.08
	}
	if ctx.showdownValueScore >= 0.28 {
		base -= 0.12
	}
	return clampFloat(base, 0.03, 0.88)
}

func opponentRangeAverages(hints []ai.OpponentRangeHint) (float64, float64, float64) {
	if len(hints) == 0 {
		return 0, 0, 0
	}
	var foldTotal float64
	var trapTotal float64
	var drawTotal float64
	var weightTotal float64
	for _, hint := range hints {
		weight := clampFloat(hint.Confidence, 0.1, 1.0)
		foldTotal += hint.FoldToPressure * weight
		trapTotal += hint.TrapRisk * weight
		drawTotal += hint.DrawWeight * weight
		weightTotal += weight
	}
	if weightTotal <= 0 {
		return 0, 0, 0
	}
	return foldTotal / weightTotal, trapTotal / weightTotal, drawTotal / weightTotal
}

func chooseBestDecisionOption(input ai.DecisionInput, baseline ai.Decision, options []ai.DecisionOption) ai.Decision {
	best := baseline
	bestRank := -1e18
	for _, option := range options {
		rank := option.EVEstimate + option.LocalScore*18.0 - option.RiskScore*8.0
		if option.IsBaseline {
			rank += 0.6
		}
		if rank > bestRank {
			bestRank = rank
			best = ai.Decision{OptionID: option.ID, Action: option.Action, Amount: option.Amount}
		}
	}
	best = materializeDecisionOption(ai.DecisionInput{DecisionOptions: options}, best)
	return guardAIDecision(input, best, baseline)
}

func materializeDecisionOption(input ai.DecisionInput, decision ai.Decision) ai.Decision {
	normalized := ai.Decision{
		OptionID: strings.ToLower(strings.TrimSpace(decision.OptionID)),
		Action:   strings.ToLower(strings.TrimSpace(decision.Action)),
		Amount:   decision.Amount,
	}
	if len(input.DecisionOptions) == 0 {
		return normalized
	}
	if normalized.OptionID != "" {
		for _, option := range input.DecisionOptions {
			if strings.EqualFold(option.ID, normalized.OptionID) {
				return ai.Decision{OptionID: option.ID, Action: option.Action, Amount: option.Amount}
			}
		}
	}
	if normalized.Action == "bet" {
		bestIdx := -1
		bestGap := int(^uint(0) >> 1)
		for idx, option := range input.DecisionOptions {
			if !strings.EqualFold(option.Action, "bet") {
				continue
			}
			gap := absInt(option.Amount - normalized.Amount)
			if gap < bestGap {
				bestGap = gap
				bestIdx = idx
			}
		}
		if bestIdx >= 0 {
			option := input.DecisionOptions[bestIdx]
			return ai.Decision{OptionID: option.ID, Action: option.Action, Amount: option.Amount}
		}
	}
	if normalized.Action != "" {
		for _, option := range input.DecisionOptions {
			if strings.EqualFold(option.Action, normalized.Action) && !strings.EqualFold(option.Action, "bet") {
				return ai.Decision{OptionID: option.ID, Action: option.Action, Amount: option.Amount}
			}
		}
	}
	return normalized
}

func roundOptionMetric(v float64) float64 {
	return math.Round(v*100) / 100
}

func uniqueSortedNotes(notes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.ToLower(strings.TrimSpace(note))
		if note == "" || seen[note] {
			continue
		}
		seen[note] = true
		out = append(out, note)
	}
	sort.Strings(out)
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
