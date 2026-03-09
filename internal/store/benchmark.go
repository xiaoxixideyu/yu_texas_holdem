package store

import (
	"context"
	"fmt"
	mathrand "math/rand"
	"sync"
	"time"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
)

type BenchmarkStatus struct {
	Running         bool            `json:"running"`
	ConfigPath      string          `json:"configPath"`
	StartedAtUnix   int64           `json:"startedAtUnix"`
	UpdatedAtUnix   int64           `json:"updatedAtUnix"`
	Iterations      int64           `json:"iterations"`
	Accepted        int64           `json:"accepted"`
	LastDeltaBB100  float64         `json:"lastDeltaBb100"`
	BestDeltaBB100  float64         `json:"bestDeltaBb100"`
	LastMessage     string          `json:"lastMessage"`
	CurrentParams   StrategyParams  `json:"currentParams"`
	PersistedParams StrategyParams  `json:"persistedParams"`
	AISettings      AIRuntimeStatus `json:"aiSettings"`
}

type BenchmarkManager struct {
	mu         sync.RWMutex
	configPath string
	cancel     context.CancelFunc
	status     BenchmarkStatus
}

func NewBenchmarkManager(configPath string) *BenchmarkManager {
	path := strategyConfigPath(configPath)
	params := currentStrategyParams()
	return &BenchmarkManager{
		configPath: path,
		status: BenchmarkStatus{
			ConfigPath:      path,
			UpdatedAtUnix:   time.Now().Unix(),
			LastMessage:     "idle",
			CurrentParams:   params,
			PersistedParams: params,
		},
	}
}

func (m *BenchmarkManager) Status() BenchmarkStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := m.status
	out.CurrentParams = currentStrategyParams()
	out.ConfigPath = m.configPath
	return out
}

func (m *BenchmarkManager) Start() (BenchmarkStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return m.status, fmt.Errorf("benchmark already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	params := currentStrategyParams()
	now := time.Now().Unix()
	m.cancel = cancel
	m.status = BenchmarkStatus{
		Running:         true,
		ConfigPath:      m.configPath,
		StartedAtUnix:   now,
		UpdatedAtUnix:   now,
		LastMessage:     "benchmark started",
		CurrentParams:   params,
		PersistedParams: params,
	}
	go m.loop(ctx, params)
	return m.status, nil
}

func (m *BenchmarkManager) Stop() BenchmarkStatus {
	m.mu.Lock()
	cancel := m.cancel
	if cancel != nil {
		cancel()
		m.cancel = nil
		m.status.Running = false
		m.status.UpdatedAtUnix = time.Now().Unix()
		m.status.LastMessage = "benchmark stopped"
		m.status.CurrentParams = currentStrategyParams()
	}
	out := m.status
	m.mu.Unlock()
	if cancel != nil {
		return out
	}
	out.CurrentParams = currentStrategyParams()
	return out
}

func (m *BenchmarkManager) loop(ctx context.Context, baseline StrategyParams) {
	rnd := mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	best := clampStrategyParams(baseline)
	bestDelta := 0.0
	iterations := int64(0)
	accepted := int64(0)
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.cancel = nil
			m.status.Running = false
			m.status.UpdatedAtUnix = time.Now().Unix()
			m.status.LastMessage = "benchmark idle"
			m.status.CurrentParams = currentStrategyParams()
			m.status.PersistedParams = best
			m.mu.Unlock()
			return
		default:
		}

		candidate := mutateStrategyParams(best, rnd)
		delta := benchmarkParamsDelta(candidate, best, rnd.Int63())
		iterations++
		message := fmt.Sprintf("iteration %d evaluated: %.2f bb/100", iterations, delta)
		if delta > 1.2 {
			confirm := benchmarkParamsDelta(candidate, best, rnd.Int63())
			delta = (delta + confirm) / 2
			message = fmt.Sprintf("iteration %d confirmed: %.2f bb/100", iterations, delta)
			if delta > 0.8 {
				best = candidate
				accepted++
				if delta > bestDelta {
					bestDelta = delta
				}
				setCurrentStrategyParams(best)
				if err := saveStrategyParamsToFile(m.configPath, best); err != nil {
					message = fmt.Sprintf("iteration %d save failed: %v", iterations, err)
				} else {
					message = fmt.Sprintf("accepted iteration %d: %.2f bb/100", iterations, delta)
				}
			}
		}

		m.mu.Lock()
		m.status.Running = true
		m.status.ConfigPath = m.configPath
		m.status.Iterations = iterations
		m.status.Accepted = accepted
		m.status.LastDeltaBB100 = delta
		m.status.BestDeltaBB100 = bestDelta
		m.status.LastMessage = message
		m.status.UpdatedAtUnix = time.Now().Unix()
		m.status.CurrentParams = currentStrategyParams()
		m.status.PersistedParams = best
		m.mu.Unlock()
	}
}

func mutateStrategyParams(base StrategyParams, rnd *mathrand.Rand) StrategyParams {
	candidate := base
	mutations := 2 + rnd.Intn(3)
	for i := 0; i < mutations; i++ {
		jitter := func(scale float64) float64 {
			return (rnd.Float64()*2 - 1) * scale
		}
		switch rnd.Intn(13) {
		case 0:
			candidate.FlopDryRangeBetReduction += jitter(0.015)
		case 1:
			candidate.TurnScareSizingWeight += jitter(0.04)
		case 2:
			candidate.RiverMissedDrawSizingWeight += jitter(0.03)
		case 3:
			candidate.RiverMissedDrawBluffWeight += jitter(0.06)
		case 4:
			candidate.RiverShowdownPenaltyWeight += jitter(0.06)
		case 5:
			candidate.RiverStationPenaltyWeight += jitter(0.06)
		case 6:
			candidate.RiverTripleBarrelBonus += jitter(0.03)
		case 7:
			candidate.RiverCheckbackStationThreshold += jitter(0.025)
		case 8:
			candidate.RiverCheckbackPairMax += jitter(0.04)
		case 9:
			candidate.RiverThinValueStationPenalty += jitter(0.04)
		case 10:
			candidate.RiverStealMissedDrawWeight += jitter(0.06)
		case 11:
			candidate.RiverStealShowdownPenalty += jitter(0.06)
		case 12:
			candidate.RiverStealStationPenalty += jitter(0.06)
		}
	}
	return clampStrategyParams(candidate)
}

func benchmarkParamsDelta(candidate StrategyParams, incumbent StrategyParams, seed int64) float64 {
	const hands = 24
	const bigBlind = 10
	profit := 0
	for hand := 0; hand < hands; hand++ {
		dealerPos := hand % 2
		handProfit, err := benchmarkSelfPlayHand(candidate, incumbent, int64(hand+1), dealerPos, seed+int64(hand)*7919)
		if err != nil {
			continue
		}
		profit += handProfit
	}
	return float64(profit) / float64(bigBlind) / float64(hands) * 100
}

func benchmarkSelfPlayHand(candidate StrategyParams, incumbent StrategyParams, handID int64, dealerPos int, seed int64) (int, error) {
	players := []*domain.GamePlayer{
		{UserID: "candidate", Username: "Candidate", IsAI: true, SeatIndex: 0, Stack: 1000},
		{UserID: "incumbent", Username: "Incumbent", IsAI: true, SeatIndex: 1, Stack: 1000},
	}
	deck := domain.NewDeck()
	rnd := mathrand.New(mathrand.NewSource(seed))
	rnd.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	game, err := domain.NewGameWithDeck(players, dealerPos, 10, 10, deck)
	if err != nil {
		return 0, err
	}
	room := &Room{
		RoomID:       fmt.Sprintf("bench-%d", seed),
		Name:         "benchmark",
		OpenBetMin:   10,
		BetMin:       10,
		Status:       RoomPlaying,
		Players:      []RoomPlayer{{UserID: "candidate", Username: "Candidate", Seat: 0, Stack: 1000, IsAI: true}, {UserID: "incumbent", Username: "Incumbent", Seat: 1, Stack: 1000, IsAI: true}},
		StateVersion: handID*100 + 1,
		HandCounter:  handID,
		Game:         game,
		AIMemory: map[string]*RoomAIMemory{
			"candidate": {HandSummaries: []string{}, OpponentProfiles: map[string]*OpponentProfile{}, OpponentStats: map[string]*OpponentStat{}},
			"incumbent": {HandSummaries: []string{}, OpponentProfiles: map[string]*OpponentProfile{}, OpponentStats: map[string]*OpponentStat{}},
		},
	}
	for actions := 0; actions < 512 && room.Game != nil && room.Game.Stage != domain.StageFinished; actions++ {
		if room.Game.TurnPos < 0 || room.Game.TurnPos >= len(room.Game.Players) {
			break
		}
		turn := room.Game.Players[room.Game.TurnPos]
		memory := room.AIMemory[turn.UserID]
		input, ok := buildAIDecisionInput(room, turn, memory)
		if !ok {
			break
		}
		params := incumbent
		if turn.UserID == "candidate" {
			params = candidate
		}
		decision := fallbackDecisionWithParams(input, params)
		decision = benchmarkSafeDecision(input, decision)
		if err := room.Game.ApplyAction(turn.UserID, decision.Action, decision.Amount); err != nil {
			fallback := benchmarkLegalDecision(input)
			if err := room.Game.ApplyAction(turn.UserID, fallback.Action, fallback.Amount); err != nil {
				return 0, err
			}
		}
		room.StateVersion++
	}
	if room.Game == nil || room.Game.Stage != domain.StageFinished {
		return 0, fmt.Errorf("benchmark hand did not finish")
	}
	for _, player := range room.Game.Players {
		if player.UserID == "candidate" {
			return player.Stack - 1000, nil
		}
	}
	return 0, fmt.Errorf("candidate not found")
}

func benchmarkSafeDecision(input ai.DecisionInput, decision ai.Decision) ai.Decision {
	if decisionAllowedByInput(input, decision) {
		return decision
	}
	return benchmarkLegalDecision(input)
}

func benchmarkLegalDecision(input ai.DecisionInput) ai.Decision {
	allowed := map[string]bool{}
	for _, action := range input.AllowedActions {
		allowed[action] = true
	}
	if allowed["check"] {
		return ai.Decision{Action: "check", Amount: 0}
	}
	if allowed["call"] {
		return ai.Decision{Action: "call", Amount: 0}
	}
	if allowed["fold"] {
		return ai.Decision{Action: "fold", Amount: 0}
	}
	if allowed["allin"] {
		return ai.Decision{Action: "allin", Amount: 0}
	}
	if allowed["bet"] {
		amount := input.MinBet
		if input.RoundBet > 0 {
			amount = input.MinRaise
		}
		if amount <= 0 {
			amount = 1
		}
		return ai.Decision{Action: "bet", Amount: clampInt(amount, 1, input.Stack)}
	}
	return ai.Decision{Action: "fold", Amount: 0}
}
