package store

import (
	"strings"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
)

func buildAIDecisionInput(room *Room, turn *domain.GamePlayer, memory *RoomAIMemory) (ai.DecisionInput, bool) {
	if room == nil || room.Game == nil || turn == nil {
		return ai.DecisionInput{}, false
	}
	allowed, callAmount, minBet, minRaise := allowedActionsForPlayer(room.Game, turn)
	if len(allowed) == 0 {
		return ai.DecisionInput{}, false
	}
	if memory == nil {
		memory = &RoomAIMemory{}
	}
	if memory.HandSummaries == nil {
		memory.HandSummaries = []string{}
	}
	if memory.OpponentProfiles == nil {
		memory.OpponentProfiles = map[string]*OpponentProfile{}
	}
	if memory.OpponentStats == nil {
		memory.OpponentStats = map[string]*OpponentStat{}
	}
	community := make([]string, 0, len(room.Game.CommunityCards))
	for _, c := range room.Game.CommunityCards {
		community = append(community, cardToText(c))
	}
	holeCards := make([]string, 0, len(turn.HoleCards))
	for _, c := range turn.HoleCards {
		holeCards = append(holeCards, cardToText(c))
	}
	handCategory, handCategoryRank, handRanks, preflopTier, madeHandStrength, drawFlags := buildHandStrengthFeatures(turn.HoleCards, room.Game.CommunityCards)
	preflopPosition := preflopPositionForPlayer(room.Game, room.Game.TurnPos)
	effectiveStackBB := effectiveStackBBForPlayer(room.Game, room.Game.TurnPos)
	preflopFacingRaise := room.Game.Stage == domain.StagePreflop && room.Game.RoundBet > room.Game.OpenBetMin
	input := ai.DecisionInput{
		RoomID:             room.RoomID,
		HandID:             room.HandCounter,
		StateVersion:       room.StateVersion,
		AIUserID:           turn.UserID,
		AIUsername:         turn.Username,
		Stage:              string(room.Game.Stage),
		Pot:                room.Game.Pot,
		RoundBet:           room.Game.RoundBet,
		OpenBetMin:         room.Game.OpenBetMin,
		BetMin:             room.Game.BetMin,
		CallAmount:         callAmount,
		MinBet:             minBet,
		MinRaise:           minRaise,
		Stack:              turn.Stack,
		AllowedActions:     allowed,
		CommunityCards:     community,
		HoleCards:          holeCards,
		HandCategory:       handCategory,
		HandCategoryRank:   handCategoryRank,
		HandRanks:          handRanks,
		PreflopTier:        preflopTier,
		PreflopPosition:    preflopPosition,
		EffectiveStackBB:   effectiveStackBB,
		PreflopFacingRaise: preflopFacingRaise,
		MadeHandStrength:   madeHandStrength,
		DrawFlags:          drawFlags,
		Players:            copyPlayers(room, room.Game),
		RecentActionLog:    copyActionLogs(room.Game.ActionLogs),
		MemorySummaries:    append([]string{}, memory.HandSummaries...),
		Profiles:           cloneProfiles(memory.OpponentProfiles),
		OpponentStats:      cloneOpponentStats(memory.OpponentStats),
	}
	input.Diagnostics = buildDecisionDiagnostics(input)
	return input, true
}

func buildDecisionDiagnostics(input ai.DecisionInput) ai.DecisionDiagnostics {
	diag := ai.DecisionDiagnostics{
		ActiveOpponents:      activeOpponentCount(input),
		PotOdds:              callPotOdds(input),
		EquityEstimate:       estimateFallbackEquity(input),
		PressureScore:        estimateFallbackPressure(input),
		BoardWetness:         boardWetness(input.CommunityCards),
		SPR:                  float64(input.Stack) / float64(maxInt(1, input.Pot)),
		FacingBet:            input.CallAmount > 0,
		HasInitiative:        heroHasInitiative(input),
		LineCapScore:         visibleRangeCapScore(input),
		StationScore:         activeOpponentCallStationScore(input),
		BarrelCount:          heroPostflopBarrelCount(input),
		PreviousBarrelCalled: previousStreetHeroBarrelCalled(input),
		VisibleTags:          []string{},
	}
	diag.StrongDraw, diag.WeakDraw = hasDrawPotential(input.DrawFlags)
	diag.ShortStack = diag.SPR <= 1.35 || input.Stack <= maxInt(input.OpenBetMin*7, input.CallAmount+input.BetMin*2)

	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	heroHole, heroBoard, cardsOK := parseDecisionCards(input)
	heroCategoryRank := input.HandCategoryRank
	if cardsOK {
		if len(heroHole)+len(heroBoard) >= 5 {
			best, _, _ := domain.BestOfSeven(append(append([]domain.Card{}, heroBoard...), heroHole...))
			heroCategoryRank = best.Category
		}
		diag.PairStrengthScore = pairStrengthScore(heroHole, heroBoard)
		diag.RangeAdvantage = boardRangeAdvantage(input, heroBoard)
		diag.ScareCardScore = latestBoardScareScore(heroBoard)
		if stage == "river" {
			diag.BlockerScore = riverHeroBlockerScore(heroHole, heroBoard, heroCategoryRank)
			diag.MissedDrawScore = riverMissedDrawScore(heroHole, heroBoard, heroCategoryRank)
			diag.ShowdownValueScore = riverShowdownValueScore(heroHole, heroBoard, heroCategoryRank, diag.PairStrengthScore)
		}
	}

	if diag.ActiveOpponents == 1 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "heads_up")
	} else if diag.ActiveOpponents >= 2 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "multiway")
	}
	if diag.FacingBet {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "facing_bet")
	}
	if diag.HasInitiative {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "initiative")
	}
	if diag.ShortStack {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "short_stack")
	}
	if input.PreflopFacingRaise {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "preflop_facing_raise")
	}
	if diag.StrongDraw {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "strong_draw")
	} else if diag.WeakDraw {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "weak_draw")
	}
	if diag.BoardWetness <= 0.35 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "dry_board")
	} else if diag.BoardWetness >= 0.58 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "wet_board")
	}
	if diag.RangeAdvantage >= 0.10 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "range_advantage")
	}
	if diag.LineCapScore >= 0.10 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "villain_capped")
	}
	if diag.ScareCardScore >= 0.10 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "scare_card")
	}
	if diag.PairStrengthScore >= 0.60 || heroCategoryRank >= 2 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "value_candidate")
	}
	if stage == "river" && diag.BlockerScore >= 0.18 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "blocker_candidate")
	}
	if stage == "river" && diag.MissedDrawScore >= 0.18 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "missed_draw_bluff_candidate")
	}
	if stage == "river" && diag.ShowdownValueScore >= 0.28 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "showdown_value")
	}
	if diag.StationScore >= 0.10 {
		diag.VisibleTags = appendVisibleTag(diag.VisibleTags, "station_risk")
	}

	return diag
}

func appendVisibleTag(tags []string, tag string) []string {
	if tag == "" {
		return tags
	}
	for _, existing := range tags {
		if existing == tag {
			return tags
		}
	}
	return append(tags, tag)
}
