package domain

import (
	"errors"
	"fmt"
	"sort"
)

type GameStage string

const (
	StagePreflop  GameStage = "preflop"
	StageFlop     GameStage = "flop"
	StageTurn     GameStage = "turn"
	StageRiver    GameStage = "river"
	StageShowdown GameStage = "showdown"
	StageFinished GameStage = "finished"
)

type GamePlayer struct {
	UserID        string
	Username      string
	SeatIndex     int
	Stack         int
	HoleCards     []Card
	Folded        bool
	AllIn         bool
	Contributed   int
	RoundContrib  int
	Won           int
	LastAction    string
	BestHandName  string
	BestHandCards []Card
}

type ActionLog struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Action   string `json:"action"`
	Amount   int    `json:"amount"`
	Stage    string `json:"stage"`
}

type GameResult struct {
	Winners []string
	Reason  string
}

type GameState struct {
	Stage          GameStage
	DealerPos      int
	SmallBlindPos  int
	BigBlindPos    int
	TurnPos        int
	Pot            int
	CommunityCards []Card
	Players        []*GamePlayer

	Deck       []Card
	DeckPos    int
	RoundBet   int
	OpenBetMin int
	BetMin     int
	HasActed   map[string]bool
	Result     *GameResult
	ActionLogs []ActionLog
}

func NewGame(players []*GamePlayer, dealerPos int, openBetMin int, betMin int) (*GameState, error) {
	if len(players) < 2 {
		return nil, errors.New("at least 2 players required")
	}
	if openBetMin <= 0 {
		return nil, errors.New("open bet min must be positive")
	}
	if betMin <= 0 {
		return nil, errors.New("bet min must be positive")
	}

	bigBlind := openBetMin
	smallBlind := openBetMin / 2
	if smallBlind < 1 {
		smallBlind = 1
	}

	var sbPos, bbPos int
	if len(players) == 2 {
		sbPos = dealerPos
		bbPos = nextEligibleSeat(players, dealerPos)
	} else {
		sbPos = nextEligibleSeat(players, dealerPos)
		bbPos = nextEligibleSeat(players, sbPos)
	}

	gs := &GameState{
		Stage:          StagePreflop,
		DealerPos:      dealerPos,
		SmallBlindPos:  sbPos,
		BigBlindPos:    bbPos,
		CommunityCards: make([]Card, 0, 5),
		Players:        players,
		Deck:           NewDeck(),
		RoundBet:       bigBlind,
		OpenBetMin:     openBetMin,
		BetMin:         betMin,
		HasActed:       map[string]bool{},
		ActionLogs:     make([]ActionLog, 0),
	}
	Shuffle(gs.Deck)
	for _, p := range gs.Players {
		p.Folded = false
		p.AllIn = false
		p.Contributed = 0
		p.RoundContrib = 0
		p.Won = 0
		p.LastAction = ""
		p.BestHandName = ""
		p.BestHandCards = nil
		p.HoleCards = []Card{gs.draw(), gs.draw()}
		gs.HasActed[p.UserID] = false
	}

	sb := gs.Players[sbPos]
	sbAmount := smallBlind
	if sb.Stack < sbAmount {
		sbAmount = sb.Stack
	}
	sb.Stack -= sbAmount
	sb.Contributed = sbAmount
	sb.RoundContrib = sbAmount
	if sb.Stack == 0 {
		sb.AllIn = true
	}
	sb.LastAction = "small_blind"
	gs.Pot += sbAmount
	gs.ActionLogs = append(gs.ActionLogs, ActionLog{UserID: sb.UserID, Username: sb.Username, Action: "small_blind", Amount: sbAmount, Stage: string(StagePreflop)})

	bb := gs.Players[bbPos]
	bbAmount := bigBlind
	if bb.Stack < bbAmount {
		bbAmount = bb.Stack
	}
	bb.Stack -= bbAmount
	bb.Contributed = bbAmount
	bb.RoundContrib = bbAmount
	if bb.Stack == 0 {
		bb.AllIn = true
	}
	bb.LastAction = "big_blind"
	gs.Pot += bbAmount
	gs.ActionLogs = append(gs.ActionLogs, ActionLog{UserID: bb.UserID, Username: bb.Username, Action: "big_blind", Amount: bbAmount, Stage: string(StagePreflop)})

	gs.TurnPos = nextTurnSeat(gs.Players, bbPos)
	gs.ensureTurnPlayable()

	return gs, nil
}

func (g *GameState) ApplyAction(userID, action string, amount int) error {
	if g.Stage == StageFinished || g.Stage == StageShowdown {
		return errors.New("game already ended")
	}
	current := g.Players[g.TurnPos]
	if current.UserID != userID {
		return errors.New("not your turn")
	}
	if current.Folded {
		return errors.New("player already folded")
	}
	if current.AllIn {
		return errors.New("player already all-in")
	}

	switch action {
	case "check":
		if g.RoundBet != current.RoundContrib {
			return errors.New("cannot check when bet exists")
		}
		current.LastAction = "check"
		g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "check", Amount: 0, Stage: string(g.Stage)})
	case "call":
		diff := g.RoundBet - current.RoundContrib
		if diff <= 0 {
			return errors.New("nothing to call")
		}
		if current.Stack <= 0 {
			return errors.New("not enough stack to call")
		}
		pay := diff
		if current.Stack < pay {
			pay = current.Stack
		}
		current.Stack -= pay
		current.Contributed += pay
		current.RoundContrib += pay
		g.Pot += pay
		if current.Stack == 0 {
			current.AllIn = true
			current.LastAction = "allin"
			g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "allin", Amount: pay, Stage: string(g.Stage)})
		} else {
			current.LastAction = "call"
			g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "call", Amount: pay, Stage: string(g.Stage)})
		}
	case "bet", "allin":
		if current.Stack <= 0 {
			return errors.New("not enough stack to bet")
		}
		commit := amount
		if action == "allin" {
			commit = current.Stack
		}
		if commit <= 0 {
			return errors.New("bet amount must be positive")
		}
		if commit > current.Stack {
			return errors.New("not enough stack to bet")
		}
		targetRoundContrib := current.RoundContrib + commit
		raises := targetRoundContrib > g.RoundBet
		if action != "allin" {
			if g.RoundBet == 0 {
				if commit < g.OpenBetMin {
					return fmt.Errorf("open bet must be at least %d", g.OpenBetMin)
				}
			} else {
				need := (g.RoundBet - current.RoundContrib) + g.BetMin
				if commit < need {
					return fmt.Errorf("raise must be at least %d", need)
				}
			}
		}
		current.Stack -= commit
		current.Contributed += commit
		current.RoundContrib += commit
		g.Pot += commit
		if raises {
			g.RoundBet = current.RoundContrib
		}
		if current.Stack == 0 || action == "allin" {
			current.AllIn = true
			current.LastAction = "allin"
			g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "allin", Amount: commit, Stage: string(g.Stage)})
		} else {
			current.LastAction = "bet"
			g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "bet", Amount: commit, Stage: string(g.Stage)})
		}
		if raises {
			for _, p := range g.activePlayers() {
				if p.UserID != current.UserID && !p.AllIn {
					g.HasActed[p.UserID] = false
				}
			}
		}
	case "fold":
		current.Folded = true
		current.LastAction = "fold"
		g.ActionLogs = append(g.ActionLogs, ActionLog{UserID: current.UserID, Username: current.Username, Action: "fold", Amount: 0, Stage: string(g.Stage)})
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}

	g.HasActed[userID] = true

	if g.countActive() == 1 {
		g.finishByLastStanding()
		return nil
	}

	if g.roundComplete() {
		g.advanceStage()
		return nil
	}

	g.TurnPos = nextTurnSeat(g.Players, g.TurnPos)
	g.ensureTurnPlayable()
	return nil
}

func (g *GameState) roundComplete() bool {
	for _, p := range g.Players {
		if p.Folded {
			continue
		}
		if p.AllIn {
			continue
		}
		if !g.HasActed[p.UserID] {
			return false
		}
		if p.RoundContrib != g.RoundBet {
			return false
		}
	}
	return true
}

func (g *GameState) advanceStage() {
	switch g.Stage {
	case StagePreflop:
		g.Stage = StageFlop
		g.CommunityCards = append(g.CommunityCards, g.draw(), g.draw(), g.draw())
	case StageFlop:
		g.Stage = StageTurn
		g.CommunityCards = append(g.CommunityCards, g.draw())
	case StageTurn:
		g.Stage = StageRiver
		g.CommunityCards = append(g.CommunityCards, g.draw())
	case StageRiver:
		g.Stage = StageShowdown
		g.finishShowdown()
		return
	}
	for _, p := range g.Players {
		p.RoundContrib = 0
		if p.Folded || p.AllIn {
			continue
		}
		g.HasActed[p.UserID] = false
	}
	g.RoundBet = 0
	if len(g.Players) == 2 {
		g.TurnPos = g.BigBlindPos
	} else {
		g.TurnPos = nextTurnSeat(g.Players, g.DealerPos)
	}
	g.ensureTurnPlayable()
}

func (g *GameState) finishByLastStanding() {
	var winner *GamePlayer
	for _, p := range g.Players {
		if !p.Folded {
			winner = p
			break
		}
	}
	if winner == nil {
		return
	}
	winner.Stack += g.Pot
	winner.Won = g.Pot
	g.Result = &GameResult{Winners: []string{winner.UserID}, Reason: "others folded"}
	g.Stage = StageFinished
}

func (g *GameState) finishShowdown() {
	active := g.activePlayers()
	if len(active) == 0 {
		g.Stage = StageFinished
		g.Result = &GameResult{Winners: nil, Reason: "no active players"}
		return
	}
	type candidate struct {
		p     *GamePlayer
		value HandValue
	}
	cands := make([]candidate, 0, len(active))
	for _, p := range active {
		cards := append([]Card{}, g.CommunityCards...)
		cards = append(cards, p.HoleCards...)
		best, bestCards, name := BestOfSeven(cards)
		p.BestHandName = name
		p.BestHandCards = bestCards
		cands = append(cands, candidate{p: p, value: best})
	}
	strength := make(map[string]HandValue, len(cands))
	for _, c := range cands {
		strength[c.p.UserID] = c.value
	}

	for _, p := range g.Players {
		p.Won = 0
	}

	for _, p := range g.Players {
		if p.Folded {
			continue
		}
		if p.Contributed > g.PotEligibleCap() {
			// no-op safeguard; normally impossible
		}
	}

	refund := g.refundUnmatchedChips()
	if refund > 0 {
		g.Pot -= refund
	}

	levels := uniqueContributionLevels(active)
	prev := 0
	for _, level := range levels {
		if level <= prev {
			continue
		}
		eligible := make([]*GamePlayer, 0, len(active))
		for _, p := range active {
			if p.Contributed >= level {
				eligible = append(eligible, p)
			}
		}
		if len(eligible) == 0 {
			prev = level
			continue
		}
		potPart := (level - prev) * len(eligible)
		if potPart <= 0 {
			prev = level
			continue
		}
		winners := bestPlayers(eligible, strength)
		share := potPart / len(winners)
		rest := potPart % len(winners)
		for i, w := range winners {
			win := share
			if i < rest {
				win++
			}
			w.Stack += win
			w.Won += win
		}
		prev = level
	}

	winnerIDs := make([]string, 0)
	for _, p := range g.Players {
		if p.Won > 0 {
			winnerIDs = append(winnerIDs, p.UserID)
		}
	}
	if len(winnerIDs) == 0 && len(active) > 0 {
		winnerIDs = append(winnerIDs, active[0].UserID)
	}
	g.Result = &GameResult{Winners: winnerIDs, Reason: "showdown"}
	g.Stage = StageFinished
}

func (g *GameState) refundUnmatchedChips() int {
	active := g.activePlayers()
	if len(active) < 2 {
		return 0
	}
	max1 := -1
	max2 := -1
	var top *GamePlayer
	for _, p := range active {
		if p.Contributed > max1 {
			max2 = max1
			max1 = p.Contributed
			top = p
		} else if p.Contributed > max2 {
			max2 = p.Contributed
		}
	}
	if top == nil || max1 <= max2 {
		return 0
	}
	refund := max1 - max2
	top.Contributed -= refund
	top.Stack += refund
	return refund
}

func uniqueContributionLevels(players []*GamePlayer) []int {
	set := map[int]struct{}{}
	for _, p := range players {
		if p.Contributed > 0 {
			set[p.Contributed] = struct{}{}
		}
	}
	out := make([]int, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func bestPlayers(players []*GamePlayer, strength map[string]HandValue) []*GamePlayer {
	if len(players) == 0 {
		return nil
	}
	best := players[0]
	winners := []*GamePlayer{best}
	for i := 1; i < len(players); i++ {
		cmp := CompareHandValue(strength[players[i].UserID], strength[best.UserID])
		if cmp > 0 {
			best = players[i]
			winners = []*GamePlayer{players[i]}
		} else if cmp == 0 {
			winners = append(winners, players[i])
		}
	}
	return winners
}

func (g *GameState) PotEligibleCap() int {
	return g.Pot
}

func (g *GameState) draw() Card {
	c := g.Deck[g.DeckPos]
	g.DeckPos++
	return c
}

func (g *GameState) activePlayers() []*GamePlayer {
	out := make([]*GamePlayer, 0, len(g.Players))
	for _, p := range g.Players {
		if !p.Folded {
			out = append(out, p)
		}
	}
	return out
}

func (g *GameState) countActive() int {
	count := 0
	for _, p := range g.Players {
		if !p.Folded {
			count++
		}
	}
	return count
}

func (g *GameState) ensureTurnPlayable() {
	for i := 0; i < len(g.Players); i++ {
		p := g.Players[g.TurnPos]
		if !p.Folded && !p.AllIn {
			return
		}
		g.TurnPos = nextTurnSeat(g.Players, g.TurnPos)
	}
	if g.roundComplete() {
		g.advanceStage()
	}
}

func (g *GameState) ForceLeaveForStore(userID string) {
	if g.Stage == StageFinished || g.Stage == StageShowdown {
		return
	}
	for _, p := range g.Players {
		if p.UserID == userID {
			p.Folded = true
			p.LastAction = "leave"
			g.HasActed[userID] = true
			break
		}
	}
	if g.countActive() <= 1 {
		g.finishByLastStanding()
		return
	}
	if g.Players[g.TurnPos].UserID == userID || g.Players[g.TurnPos].Folded || g.Players[g.TurnPos].AllIn {
		g.TurnPos = nextTurnSeat(g.Players, g.TurnPos)
	}
	g.ensureTurnPlayable()
	if g.roundComplete() {
		g.advanceStage()
	}
}

func (g *GameState) CountActiveForStore() int {
	return g.countActive()
}

func (g *GameState) FinishByLastStandingForStore() {
	g.finishByLastStanding()
}

func nextEligibleSeat(players []*GamePlayer, pos int) int {
	n := len(players)
	for i := 1; i <= n; i++ {
		next := (pos + i) % n
		if !players[next].Folded {
			return next
		}
	}
	return pos
}

func nextTurnSeat(players []*GamePlayer, pos int) int {
	n := len(players)
	for i := 1; i <= n; i++ {
		next := (pos + i) % n
		if !players[next].Folded && !players[next].AllIn {
			return next
		}
	}
	return pos
}
