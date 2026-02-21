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
	OpenBetMin int // 大盲金额
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

	// 计算大小盲位置
	var sbPos, bbPos int
	if len(players) == 2 {
		// Heads-up: 庄家 = 小盲，另一位 = 大盲
		sbPos = dealerPos
		bbPos = nextActiveSeat(players, dealerPos)
	} else {
		// 3+人: 庄家下一位 = 小盲，再下一位 = 大盲
		sbPos = nextActiveSeat(players, dealerPos)
		bbPos = nextActiveSeat(players, sbPos)
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
		p.Contributed = 0
		p.RoundContrib = 0
		p.Won = 0
		p.LastAction = ""
		p.BestHandName = ""
		p.BestHandCards = nil
		p.HoleCards = []Card{gs.draw(), gs.draw()}
		gs.HasActed[p.UserID] = false
	}

	// 小盲下注
	sb := gs.Players[sbPos]
	sbAmount := smallBlind
	if sb.Stack < sbAmount {
		sbAmount = sb.Stack
	}
	sb.Stack -= sbAmount
	sb.Contributed = sbAmount
	sb.RoundContrib = sbAmount
	sb.LastAction = "small_blind"
	gs.Pot += sbAmount
	gs.ActionLogs = append(gs.ActionLogs, ActionLog{
		UserID: sb.UserID, Username: sb.Username,
		Action: "small_blind", Amount: sbAmount, Stage: string(StagePreflop),
	})

	// 大盲下注
	bb := gs.Players[bbPos]
	bbAmount := bigBlind
	if bb.Stack < bbAmount {
		bbAmount = bb.Stack
	}
	bb.Stack -= bbAmount
	bb.Contributed = bbAmount
	bb.RoundContrib = bbAmount
	bb.LastAction = "big_blind"
	gs.Pot += bbAmount
	gs.ActionLogs = append(gs.ActionLogs, ActionLog{
		UserID: bb.UserID, Username: bb.Username,
		Action: "big_blind", Amount: bbAmount, Stage: string(StagePreflop),
	})

	// 翻牌前行动从大盲下一位开始
	gs.TurnPos = nextActiveSeat(players, bbPos)

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

	switch action {
	case "check":
		if g.RoundBet != current.RoundContrib {
			return errors.New("cannot check when bet exists")
		}
		current.LastAction = "check"
		g.ActionLogs = append(g.ActionLogs, ActionLog{
			UserID: current.UserID, Username: current.Username,
			Action: "check", Amount: 0, Stage: string(g.Stage),
		})
	case "call":
		diff := g.RoundBet - current.RoundContrib
		if diff <= 0 {
			return errors.New("nothing to call")
		}
		if current.Stack < diff {
			return errors.New("not enough stack to call")
		}
		current.Stack -= diff
		current.Contributed += diff
		current.RoundContrib += diff
		g.Pot += diff
		current.LastAction = "call"
		g.ActionLogs = append(g.ActionLogs, ActionLog{
			UserID: current.UserID, Username: current.Username,
			Action: "call", Amount: diff, Stage: string(g.Stage),
		})
	case "bet":
		if amount <= 0 {
			return errors.New("bet amount must be positive")
		}
		if g.RoundBet == 0 {
			if amount < g.OpenBetMin {
				return fmt.Errorf("open bet must be at least %d", g.OpenBetMin)
			}
		} else {
			need := (g.RoundBet - current.RoundContrib) + g.BetMin
			if amount < need {
				return fmt.Errorf("raise must be at least %d", need)
			}
		}
		if current.Stack < amount {
			return errors.New("not enough stack to bet")
		}
		current.Stack -= amount
		current.Contributed += amount
		current.RoundContrib += amount
		g.Pot += amount
		g.RoundBet = current.RoundContrib
		current.LastAction = "bet"
		g.ActionLogs = append(g.ActionLogs, ActionLog{
			UserID: current.UserID, Username: current.Username,
			Action: "bet", Amount: amount, Stage: string(g.Stage),
		})
		for _, p := range g.activePlayers() {
			if p.UserID != current.UserID {
				g.HasActed[p.UserID] = false
			}
		}
	case "fold":
		current.Folded = true
		current.LastAction = "fold"
		g.ActionLogs = append(g.ActionLogs, ActionLog{
			UserID: current.UserID, Username: current.Username,
			Action: "fold", Amount: 0, Stage: string(g.Stage),
		})
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

	g.TurnPos = nextActiveSeat(g.Players, g.TurnPos)
	return nil
}

func (g *GameState) roundComplete() bool {
	for _, p := range g.Players {
		if p.Folded {
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
		if p.Folded {
			continue
		}
		g.HasActed[p.UserID] = false
	}
	g.RoundBet = 0
	g.TurnPos = nextActiveSeat(g.Players, g.DealerPos)
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
	sort.Slice(cands, func(i, j int) bool {
		return CompareHandValue(cands[i].value, cands[j].value) > 0
	})
	best := cands[0].value
	winners := []*GamePlayer{cands[0].p}
	for i := 1; i < len(cands); i++ {
		if CompareHandValue(cands[i].value, best) == 0 {
			winners = append(winners, cands[i].p)
		}
	}
	share := 0
	if len(winners) > 0 {
		share = g.Pot / len(winners)
	}
	winnerIDs := make([]string, 0, len(winners))
	for _, w := range winners {
		w.Stack += share
		w.Won = share
		winnerIDs = append(winnerIDs, w.UserID)
	}
	g.Result = &GameResult{Winners: winnerIDs, Reason: "showdown"}
	g.Stage = StageFinished
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
	if g.Players[g.TurnPos].UserID == userID || g.Players[g.TurnPos].Folded {
		g.TurnPos = nextActiveSeat(g.Players, g.TurnPos)
	}
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

func nextActiveSeat(players []*GamePlayer, pos int) int {
	n := len(players)
	for i := 1; i <= n; i++ {
		next := (pos + i) % n
		if !players[next].Folded {
			return next
		}
	}
	return pos
}
