package store

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	mathrand "math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"texas_yu/internal/ai"
	"texas_yu/internal/domain"
)

type Session struct {
	UserID    string `json:"userId"`
	Username  string `json:"username"`
	ExpiresAt int64  `json:"expiresAt"`
}

type RoomStatus string

const (
	RoomWaiting RoomStatus = "waiting"
	RoomPlaying RoomStatus = "playing"
)

const (
	QuickChatBubbleTTLMS int64 = 5000
	QuickChatCooldownMS  int64 = 6000
	QuickChatRetentionMS int64 = 7000
)

const (
	MaxAISummaries     = 20
	DefaultPlayerStack = 10000
)

type QuickChatEvent struct {
	EventID     int64  `json:"eventId"`
	UserID      string `json:"userId"`
	Username    string `json:"username"`
	PhraseID    string `json:"phraseId"`
	CreatedAtMs int64  `json:"createdAtMs"`
	ExpireAtMs  int64  `json:"expireAtMs"`
}

var quickChatPhraseList = []string{
	"wait_flowers",
	"solve_universe",
	"tea_refill",
	"countdown",
	"thinker_mode",
	"dawn_table",
	"cappuccino",
	"showtime",
	"you_act_i_act",
	"something_here",
	"mind_game",
	"script_seen",
	"allin_warning",
	"just_this",
	"easy_sigh",
	"fold_now",
	"you_call_i_show",
	"take_the_shot",
	"pressure_on",
	"tilt_alert",
	"nh",
	"gg",
	"luck_is_skill",
	"next_real",
}

var quickChatPhrases = map[string]struct{}{
	"wait_flowers":    {},
	"solve_universe":  {},
	"tea_refill":      {},
	"countdown":       {},
	"thinker_mode":    {},
	"dawn_table":      {},
	"cappuccino":      {},
	"showtime":        {},
	"you_act_i_act":   {},
	"something_here":  {},
	"mind_game":       {},
	"script_seen":     {},
	"allin_warning":   {},
	"just_this":       {},
	"easy_sigh":       {},
	"fold_now":        {},
	"you_call_i_show": {},
	"take_the_shot":   {},
	"pressure_on":     {},
	"tilt_alert":      {},
	"nh":              {},
	"gg":              {},
	"luck_is_skill":   {},
	"next_real":       {},
}

type OpponentProfile struct {
	Style      string   `json:"style"`
	Tendencies []string `json:"tendencies"`
	Advice     string   `json:"advice"`
}

type OpponentStat struct {
	Hands               int `json:"hands"`
	VPIPHands           int `json:"vpipHands"`
	PFRHands            int `json:"pfrHands"`
	PostflopAggActions  int `json:"postflopAggActions"`
	PostflopCallActions int `json:"postflopCallActions"`
	FoldActions         int `json:"foldActions"`
	DecisionActions     int `json:"decisionActions"`
	WentToShowdownHands int `json:"wentToShowdownHands"`
	WonShowdownHands    int `json:"wonShowdownHands"`
}

type RoomAIMemory struct {
	HandSummaries      []string                    `json:"handSummaries"`
	OpponentProfiles   map[string]*OpponentProfile `json:"opponentProfiles"`
	OpponentStats      map[string]*OpponentStat    `json:"opponentStats"`
	LastSummarizedHand int64                       `json:"lastSummarizedHand"`
	LastStatsHand      int64                       `json:"lastStatsHand"`
	LastDecisionHand   int64                       `json:"-"`
}

type RoomPlayer struct {
	UserID    string `json:"userId"`
	Username  string `json:"username"`
	Seat      int    `json:"seat"`
	Stack     int    `json:"stack"`
	IsAI      bool   `json:"isAi"`
	AIManaged bool   `json:"aiManaged"`
}

type RoomSpectator struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
}

type ChipRefreshVoteResult string

const (
	ChipRefreshVotePending  ChipRefreshVoteResult = "pending"
	ChipRefreshVoteApproved ChipRefreshVoteResult = "approved"
	ChipRefreshVoteRejected ChipRefreshVoteResult = "rejected"
)

type ChipRefreshVoteDecision string

const (
	ChipRefreshVoteAgree  ChipRefreshVoteDecision = "agree"
	ChipRefreshVoteReject ChipRefreshVoteDecision = "reject"
)

type ChipRefreshVote struct {
	StartedByUserID string                             `json:"startedByUserId"`
	EligibleUserIDs []string                           `json:"eligibleUserIds"`
	Votes           map[string]ChipRefreshVoteDecision `json:"votes"`
	Result          ChipRefreshVoteResult              `json:"result"`
	UpdatedAtUnix   int64                              `json:"updatedAtUnix"`
}

type Room struct {
	RoomID               string          `json:"roomId"`
	Name                 string          `json:"name"`
	OpenBetMin           int             `json:"openBetMin"`
	BetMin               int             `json:"betMin"`
	OwnerUserID          string          `json:"ownerUserId"`
	Status               RoomStatus      `json:"status"`
	Players              []RoomPlayer    `json:"players"`
	Spectators           []RoomSpectator `json:"spectators,omitempty"`
	StateVersion         int64           `json:"stateVersion"`
	UpdatedAtUnix        int64           `json:"updatedAtUnix"`
	NextDealerPos        int             `json:"-"`
	Game                 *domain.GameState
	ActionSeen           map[string]bool
	QuickChats           []QuickChatEvent
	QuickChatSeen        map[string]bool
	QuickChatSeenOrder   []quickChatSeenKey
	QuickChatLastSentAt  map[string]int64
	QuickChatNextEventID int64
	AIMemory             map[string]*RoomAIMemory `json:"aiMemory"`
	ChipRefreshVote      *ChipRefreshVote         `json:"chipRefreshVote,omitempty"`
	HandCounter          int64
}

type quickChatSeenKey struct {
	ActionID    string
	CreatedAtMs int64
}

type aiJobType string

const (
	aiJobDecide    aiJobType = "decide"
	aiJobSummarize aiJobType = "summarize"
)

type aiDecisionTask struct {
	RoomID          string
	HandID          int64
	ExpectedVersion int64
	AIUserID        string
	ActionID        string
	Input           ai.DecisionInput
	Fallback        ai.Decision
	RetriesLeft     int
}

type aiSummaryTask struct {
	RoomID string
	HandID int64
	Input  ai.SummaryInput
}

type aiTaskEnvelope struct {
	kind    aiJobType
	decide  *aiDecisionTask
	summary *aiSummaryTask
}

type Options struct {
	AI                  ai.Service
	AIConfig            ai.Config
	StrategyConfigPath  string
	AIRuntimeConfigPath string
}

type MemoryStore struct {
	mu                  sync.RWMutex
	aiStateMu           sync.RWMutex
	users               map[string]*Session
	rooms               map[string]*Room
	lastActive          map[string]int64
	nextRoom            int64
	nextAIUser          int64
	roomsVersion        int64
	strategyConfigPath  string
	aiRuntimeConfigPath string
	aiRuntimeUpdatedAt  int64
	aiRuntimePersist    bool
	aiBaseConfig        ai.Config
	aiBaseService       ai.Service
	aiRuntimeSettings   AIRuntimeSettings

	aiService ai.Service
	aiWorkers map[string]bool
	aiQueue   chan aiTaskEnvelope
	benchmark *BenchmarkManager
}

func NewMemoryStore(opts ...Options) *MemoryStore {
	var cfg Options
	if len(opts) > 0 {
		cfg = opts[0]
	}
	aiSvc := cfg.AI
	if aiSvc == nil {
		aiSvc = ai.NoopService{}
	}
	explicitConfigPath := strings.TrimSpace(cfg.StrategyConfigPath)
	configPath := strategyConfigPath(explicitConfigPath)
	if explicitConfigPath != "" {
		if params, exists, err := loadStrategyParamsFromFile(configPath); err == nil {
			setCurrentStrategyParams(params)
			if !exists {
				_ = saveStrategyParamsToFile(configPath, params)
			}
		} else {
			_ = saveStrategyParamsToFile(configPath, currentStrategyParams())
		}
	}
	runtimeDefaults := defaultAIRuntimeSettings(cfg.AIConfig, aiSvc)
	explicitAIRuntimePath := strings.TrimSpace(cfg.AIRuntimeConfigPath)
	runtimePath := aiRuntimeConfigPath(explicitAIRuntimePath)
	runtimeSettings := runtimeDefaults
	runtimeUpdatedAt := time.Now().Unix()
	if explicitAIRuntimePath != "" {
		if loaded, exists, err := loadAIRuntimeSettingsFromFile(runtimePath, runtimeDefaults); err == nil {
			runtimeSettings = loaded
			if !exists {
				_ = saveAIRuntimeSettingsToFile(runtimePath, runtimeSettings)
			}
		} else {
			_ = saveAIRuntimeSettingsToFile(runtimePath, runtimeSettings)
		}
	}
	ms := &MemoryStore{
		users:               map[string]*Session{},
		rooms:               map[string]*Room{},
		lastActive:          map[string]int64{},
		aiWorkers:           map[string]bool{},
		aiQueue:             make(chan aiTaskEnvelope, 256),
		strategyConfigPath:  configPath,
		aiRuntimeConfigPath: runtimePath,
		aiRuntimeUpdatedAt:  runtimeUpdatedAt,
		aiRuntimePersist:    explicitAIRuntimePath != "",
		aiBaseConfig:        cfg.AIConfig,
		aiBaseService:       aiSvc,
		aiRuntimeSettings:   runtimeSettings,
	}
	ms.rebuildAIServiceLocked()
	ms.benchmark = NewBenchmarkManager(configPath)
	go ms.idleCleanupLoop()
	go ms.aiEventLoop()
	return ms
}

func (m *MemoryStore) rebuildAIServiceLocked() {
	cfg := m.aiBaseConfig
	if model := strings.TrimSpace(m.aiRuntimeSettings.Model); model != "" {
		cfg.Model = model
	}
	if !m.aiRuntimeSettings.UseLLM {
		m.aiService = ai.NoopService{}
		return
	}
	if cfg.Enabled() {
		m.aiService = ai.NewService(cfg)
		return
	}
	if m.aiBaseService != nil && m.aiBaseService.Enabled() {
		m.aiService = m.aiBaseService
		return
	}
	m.aiService = ai.NoopService{}
}

func (m *MemoryStore) currentAIService() ai.Service {
	if m == nil {
		return ai.NoopService{}
	}
	m.aiStateMu.RLock()
	defer m.aiStateMu.RUnlock()
	if m.aiService == nil {
		return ai.NoopService{}
	}
	return m.aiService
}

func (m *MemoryStore) currentAISettings() AIRuntimeStatus {
	status := AIRuntimeStatus{ConfigPath: aiRuntimeConfigPath("")}
	if m == nil {
		status.DecisionMode = "offline"
		return status
	}
	m.aiStateMu.RLock()
	defer m.aiStateMu.RUnlock()
	cfg := m.aiBaseConfig
	model := strings.TrimSpace(m.aiRuntimeSettings.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.Model)
	}
	usingLLM := m.aiRuntimeSettings.UseLLM && m.aiService != nil && m.aiService.Enabled()
	status = AIRuntimeStatus{
		ConfigPath:        m.aiRuntimeConfigPath,
		UseLLM:            m.aiRuntimeSettings.UseLLM,
		Model:             model,
		DecisionMode:      "offline",
		LLMConfigured:     usingLLM,
		APIKeyConfigured:  strings.TrimSpace(cfg.APIKey) != "",
		BaseURL:           strings.TrimSpace(cfg.BaseURL),
		APIFormat:         string(cfg.APIFormat),
		TimeoutMs:         cfg.Timeout.Milliseconds(),
		MaxRetry:          cfg.MaxRetry,
		LastUpdatedAtUnix: m.aiRuntimeUpdatedAt,
	}
	if usingLLM {
		status.DecisionMode = "llm"
	}
	return status
}

func (m *MemoryStore) UpdateAIRuntimeSettings(settings AIRuntimeSettings) (AIRuntimeStatus, error) {
	if m == nil {
		return AIRuntimeStatus{}, fmt.Errorf("ai runtime unavailable")
	}
	settings.Model = strings.TrimSpace(settings.Model)
	m.aiStateMu.Lock()
	cfg := m.aiBaseConfig
	if settings.UseLLM && settings.Model == "" && strings.TrimSpace(cfg.Model) == "" {
		m.aiStateMu.Unlock()
		return AIRuntimeStatus{}, fmt.Errorf("model required when enabling llm")
	}
	m.aiRuntimeSettings = AIRuntimeSettings{UseLLM: settings.UseLLM, Model: settings.Model}
	m.aiRuntimeUpdatedAt = time.Now().Unix()
	m.rebuildAIServiceLocked()
	path := m.aiRuntimeConfigPath
	persist := m.aiRuntimePersist
	status := AIRuntimeStatus{
		ConfigPath:        m.aiRuntimeConfigPath,
		UseLLM:            m.aiRuntimeSettings.UseLLM,
		Model:             strings.TrimSpace(m.aiRuntimeSettings.Model),
		DecisionMode:      "offline",
		LLMConfigured:     m.aiService != nil && m.aiService.Enabled(),
		APIKeyConfigured:  strings.TrimSpace(cfg.APIKey) != "",
		BaseURL:           strings.TrimSpace(cfg.BaseURL),
		APIFormat:         string(cfg.APIFormat),
		TimeoutMs:         cfg.Timeout.Milliseconds(),
		MaxRetry:          cfg.MaxRetry,
		LastUpdatedAtUnix: m.aiRuntimeUpdatedAt,
	}
	if status.Model == "" {
		status.Model = strings.TrimSpace(cfg.Model)
	}
	if status.LLMConfigured {
		status.DecisionMode = "llm"
	}
	m.aiStateMu.Unlock()
	if persist {
		if err := saveAIRuntimeSettingsToFile(path, settings); err != nil {
			return status, err
		}
	}
	return m.currentAISettings(), nil
}

func (m *MemoryStore) decisionLLMEnabled() bool {
	service := m.currentAIService()
	return service != nil && service.Enabled()
}

func (m *MemoryStore) summaryLLMEnabled() bool {
	service := m.currentAIService()
	return service != nil && service.Enabled()
}

func (m *MemoryStore) CreateSession(username string) *Session {
	now := time.Now().Unix()
	s := &Session{
		UserID:    newUserID(),
		Username:  username,
		ExpiresAt: now + 24*3600,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[s.UserID] = s
	m.lastActive[s.UserID] = now
	return s
}

func (m *MemoryStore) GetUser(userID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.users[userID]
	if !ok {
		return nil, false
	}
	if s.ExpiresAt <= time.Now().Unix() {
		return nil, false
	}
	return s, true
}

func newUserID() string {
	b := make([]byte, 8)
	if _, err := cryptorand.Read(b); err != nil {
		return fmt.Sprintf("u-fallback-%d", time.Now().UnixNano())
	}
	return "u-" + hex.EncodeToString(b)
}

func (m *MemoryStore) newAIUserID() string {
	next := atomic.AddInt64(&m.nextAIUser, 1)
	return fmt.Sprintf("ai-%d", next)
}

func (m *MemoryStore) ListRooms() ([]Room, int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		copyRoom := cloneRoomLocked(r)
		if copyRoom == nil {
			continue
		}
		copyRoom.Game = nil
		list = append(list, *copyRoom)
	}
	return list, m.roomsVersion
}

func (m *MemoryStore) CreateRoom(owner *Session, name string, openBetMin int, betMin int) *Room {
	rid := atomic.AddInt64(&m.nextRoom, 1)
	r := &Room{
		RoomID:               fmt.Sprintf("r-%d", rid),
		Name:                 name,
		OpenBetMin:           openBetMin,
		BetMin:               betMin,
		OwnerUserID:          owner.UserID,
		Status:               RoomWaiting,
		Players:              []RoomPlayer{{UserID: owner.UserID, Username: owner.Username, Seat: 0, Stack: DefaultPlayerStack, IsAI: false, AIManaged: false}},
		Spectators:           []RoomSpectator{},
		StateVersion:         1,
		UpdatedAtUnix:        time.Now().Unix(),
		NextDealerPos:        0,
		Game:                 nil,
		ActionSeen:           map[string]bool{},
		QuickChats:           []QuickChatEvent{},
		QuickChatSeen:        map[string]bool{},
		QuickChatSeenOrder:   []quickChatSeenKey{},
		QuickChatLastSentAt:  map[string]int64{},
		QuickChatNextEventID: 0,
		AIMemory:             map[string]*RoomAIMemory{},
		HandCounter:          0,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.rooms[r.RoomID] = r
	m.roomsVersion++
	return r
}

func (m *MemoryStore) JoinRoom(roomID string, s *Session) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isPlayer(r, s.UserID) {
		return r, nil
	}
	if r.Status != RoomWaiting {
		return nil, errors.New("room already playing")
	}
	if idx := spectatorIndex(r, s.UserID); idx >= 0 {
		r.Spectators = append(r.Spectators[:idx], r.Spectators[idx+1:]...)
	}
	r.Players = append(r.Players, RoomPlayer{UserID: s.UserID, Username: s.Username, Seat: len(r.Players), Stack: DefaultPlayerStack, IsAI: false, AIManaged: false})
	r.ChipRefreshVote = nil
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) SpectateRoom(roomID string, s *Session) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isPlayer(r, s.UserID) {
		return r, nil
	}
	if isSpectator(r, s.UserID) {
		return r, nil
	}
	r.Spectators = append(r.Spectators, RoomSpectator{UserID: s.UserID, Username: s.Username})
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) AddAI(roomID, ownerUserID, name string) (*Room, *RoomPlayer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, nil, errors.New("room not found")
	}
	if isSpectator(r, ownerUserID) {
		return nil, nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, ownerUserID) {
		return nil, nil, errors.New("user not in room")
	}
	if r.OwnerUserID != ownerUserID {
		return nil, nil, errors.New("only owner can add ai")
	}
	if r.Status != RoomWaiting {
		return nil, nil, errors.New("can only add ai in waiting")
	}
	aiName := strings.TrimSpace(name)
	if aiName == "" {
		aiName = fmt.Sprintf("Bot %d", len(r.Players)+1)
	}
	aiPlayer := RoomPlayer{
		UserID:    m.newAIUserID(),
		Username:  aiName,
		Seat:      len(r.Players),
		Stack:     DefaultPlayerStack,
		IsAI:      true,
		AIManaged: false,
	}
	r.Players = append(r.Players, aiPlayer)
	r.AIMemory[aiPlayer.UserID] = &RoomAIMemory{
		HandSummaries:    []string{},
		OpponentProfiles: map[string]*OpponentProfile{},
		OpponentStats:    map[string]*OpponentStat{},
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, &aiPlayer, nil
}

func (m *MemoryStore) RemoveAI(roomID, ownerUserID, aiUserID string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isSpectator(r, ownerUserID) {
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, ownerUserID) {
		return nil, errors.New("user not in room")
	}
	if r.OwnerUserID != ownerUserID {
		return nil, errors.New("only owner can remove ai")
	}
	if r.Status != RoomWaiting {
		return nil, errors.New("can only remove ai in waiting")
	}
	idx := -1
	for i, p := range r.Players {
		if p.UserID == aiUserID && p.IsAI {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errors.New("ai not found")
	}
	r.Players = append(r.Players[:idx], r.Players[idx+1:]...)
	for i := range r.Players {
		r.Players[i].Seat = i
	}
	delete(r.AIMemory, aiUserID)
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) SetPlayerAIManaged(roomID, userID string, enabled bool) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		return nil, errors.New("spectator is read-only")
	}
	idx := playerIndex(r, userID)
	if idx < 0 {
		return nil, errors.New("user not in room")
	}
	if r.Players[idx].IsAI {
		return nil, errors.New("ai player cannot toggle ai managed")
	}
	if r.Players[idx].AIManaged == enabled {
		return r, nil
	}

	r.Players[idx].AIManaged = enabled
	if r.Game != nil {
		for _, gp := range r.Game.Players {
			if gp.UserID == userID {
				gp.AIManaged = enabled
				break
			}
		}
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	m.enqueueAIDecisionLocked(r)
	return r, nil
}

func cloneRoomLocked(r *Room) *Room {
	if r == nil {
		return nil
	}
	copyRoom := *r
	if r.Players != nil {
		copyRoom.Players = append([]RoomPlayer(nil), r.Players...)
	}
	if r.Spectators != nil {
		copyRoom.Spectators = append([]RoomSpectator(nil), r.Spectators...)
	}
	if r.AIMemory != nil {
		copyRoom.AIMemory = map[string]*RoomAIMemory{}
		for uid, mem := range r.AIMemory {
			if mem == nil {
				copyRoom.AIMemory[uid] = nil
				continue
			}
			m2 := &RoomAIMemory{
				HandSummaries:      append([]string(nil), mem.HandSummaries...),
				LastSummarizedHand: mem.LastSummarizedHand,
				LastStatsHand:      mem.LastStatsHand,
				LastDecisionHand:   mem.LastDecisionHand,
			}
			if mem.OpponentProfiles != nil {
				m2.OpponentProfiles = map[string]*OpponentProfile{}
				for opID, op := range mem.OpponentProfiles {
					if op == nil {
						m2.OpponentProfiles[opID] = nil
						continue
					}
					m2.OpponentProfiles[opID] = &OpponentProfile{
						Style:      op.Style,
						Tendencies: append([]string(nil), op.Tendencies...),
						Advice:     op.Advice,
					}
				}
			}
			if mem.OpponentStats != nil {
				m2.OpponentStats = map[string]*OpponentStat{}
				for opID, st := range mem.OpponentStats {
					if st == nil {
						m2.OpponentStats[opID] = nil
						continue
					}
					copied := *st
					m2.OpponentStats[opID] = &copied
				}
			}
			copyRoom.AIMemory[uid] = m2
		}
	}
	if r.ChipRefreshVote != nil {
		voteCopy := *r.ChipRefreshVote
		voteCopy.EligibleUserIDs = append([]string(nil), r.ChipRefreshVote.EligibleUserIDs...)
		if r.ChipRefreshVote.Votes != nil {
			voteCopy.Votes = map[string]ChipRefreshVoteDecision{}
			for uid, decision := range r.ChipRefreshVote.Votes {
				voteCopy.Votes[uid] = decision
			}
		}
		copyRoom.ChipRefreshVote = &voteCopy
	}
	if r.Game != nil {
		gCopy := *r.Game
		if r.Game.CommunityCards != nil {
			gCopy.CommunityCards = append([]domain.Card(nil), r.Game.CommunityCards...)
		}
		if r.Game.ActionLogs != nil {
			gCopy.ActionLogs = append([]domain.ActionLog(nil), r.Game.ActionLogs...)
		}
		if r.Game.Players != nil {
			gCopy.Players = make([]*domain.GamePlayer, len(r.Game.Players))
			for i, gp := range r.Game.Players {
				if gp == nil {
					continue
				}
				pCopy := *gp
				if gp.HoleCards != nil {
					pCopy.HoleCards = append([]domain.Card(nil), gp.HoleCards...)
				}
				if gp.BestHandCards != nil {
					pCopy.BestHandCards = append([]domain.Card(nil), gp.BestHandCards...)
				}
				gCopy.Players[i] = &pCopy
			}
		}
		if r.Game.Result != nil {
			resultCopy := *r.Game.Result
			resultCopy.Winners = append([]string(nil), r.Game.Result.Winners...)
			gCopy.Result = &resultCopy
		}
		copyRoom.Game = &gCopy
	}
	return &copyRoom
}

func (m *MemoryStore) GetRoom(roomID string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, false
	}
	return cloneRoomLocked(r), true
}

func (m *MemoryStore) buildGameFromRoom(r *Room, stacks map[string]int) (*domain.GameState, error) {
	gps := make([]*domain.GamePlayer, 0, len(r.Players))
	playablePosToGamePos := map[int]int{}
	for pos, p := range r.Players {
		stack := p.Stack
		if stacks != nil {
			if v, ok := stacks[p.UserID]; ok {
				stack = v
			}
		}
		if stack <= 0 {
			continue
		}
		playablePosToGamePos[pos] = len(gps)
		gps = append(gps, &domain.GamePlayer{
			UserID:    p.UserID,
			Username:  p.Username,
			IsAI:      p.IsAI,
			AIManaged: p.AIManaged,
			SeatIndex: p.Seat,
			Stack:     stack,
		})
	}
	dealerPos := 0
	if len(gps) > 0 && len(r.Players) > 0 {
		startPos := ((r.NextDealerPos % len(r.Players)) + len(r.Players)) % len(r.Players)
		for i := 0; i < len(r.Players); i++ {
			roomPos := (startPos + i) % len(r.Players)
			if pos, ok := playablePosToGamePos[roomPos]; ok {
				dealerPos = pos
				break
			}
		}
	}
	return domain.NewGame(gps, dealerPos, r.OpenBetMin, r.BetMin)
}

func nextDealerPosAfterStart(room *Room, game *domain.GameState) int {
	if room == nil || len(room.Players) == 0 {
		return 0
	}
	fallback := ((room.NextDealerPos % len(room.Players)) + len(room.Players)) % len(room.Players)
	if game == nil || len(game.Players) == 0 {
		return (fallback + 1) % len(room.Players)
	}
	dealerPos := game.DealerPos
	if dealerPos < 0 || dealerPos >= len(game.Players) {
		return (fallback + 1) % len(room.Players)
	}
	dealerSeat := game.Players[dealerPos].SeatIndex
	if dealerSeat < 0 || dealerSeat >= len(room.Players) {
		return (fallback + 1) % len(room.Players)
	}
	return (dealerSeat + 1) % len(room.Players)
}

func (m *MemoryStore) StartGame(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("user not in room")
	}
	if r.OwnerUserID != userID {
		m.mu.Unlock()
		return nil, errors.New("only owner can start")
	}
	if r.Status != RoomWaiting {
		m.mu.Unlock()
		return nil, errors.New("game already started")
	}
	if len(r.Players) < 2 {
		m.mu.Unlock()
		return nil, errors.New("at least 2 players needed")
	}
	g, err := m.buildGameFromRoom(r, nil)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	r.Game = g
	r.Status = RoomPlaying
	r.ActionSeen = map[string]bool{}
	r.ChipRefreshVote = nil
	r.HandCounter++
	if len(r.Players) > 0 {
		r.NextDealerPos = nextDealerPosAfterStart(r, g)
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	m.enqueueAIDecisionLocked(r)
	m.mu.Unlock()
	return r, nil
}

func (m *MemoryStore) LeaveRoom(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
	}
	idx := playerIndex(r, userID)
	if idx < 0 {
		spectatorIdx := spectatorIndex(r, userID)
		if spectatorIdx < 0 {
			m.mu.Unlock()
			return nil, errors.New("user not in room")
		}
		r.Spectators = append(r.Spectators[:spectatorIdx], r.Spectators[spectatorIdx+1:]...)
		r.StateVersion++
		r.UpdatedAtUnix = time.Now().Unix()
		m.roomsVersion++
		m.mu.Unlock()
		return r, nil
	}

	finishedByLeave := false
	if r.Status == RoomPlaying && r.Game != nil {
		beforeStage := r.Game.Stage
		r.Game.ForceLeaveForStore(userID)
		finishedByLeave = beforeStage != domain.StageFinished && r.Game.Stage == domain.StageFinished
	}

	r.Players = append(r.Players[:idx], r.Players[idx+1:]...)
	for i := range r.Players {
		r.Players[i].Seat = i
	}
	if r.AIMemory != nil {
		delete(r.AIMemory, userID)
	}
	r.ChipRefreshVote = nil

	if countHumans(r.Players) == 0 {
		delete(m.rooms, roomID)
		delete(m.aiWorkers, roomID)
		m.roomsVersion++
		m.mu.Unlock()
		return nil, nil
	}
	if r.OwnerUserID == userID {
		r.OwnerUserID = firstHumanOwner(r.Players)
	}

	if r.Game != nil {
		filtered := make([]*domain.GamePlayer, 0, len(r.Game.Players))
		for _, gp := range r.Game.Players {
			if gp.UserID != userID {
				filtered = append(filtered, gp)
			}
		}
		playersByUserID := make(map[string]*domain.GamePlayer, len(filtered))
		for _, gp := range filtered {
			playersByUserID[gp.UserID] = gp
		}
		reordered := make([]*domain.GamePlayer, 0, len(r.Players))
		for _, rp := range r.Players {
			if gp, ok := playersByUserID[rp.UserID]; ok {
				gp.SeatIndex = rp.Seat
				reordered = append(reordered, gp)
			}
		}
		r.Game.Players = reordered
		if len(r.Game.Players) > 0 {
			if r.Game.TurnPos >= len(r.Game.Players) {
				r.Game.TurnPos = 0
			}
			if r.Game.DealerPos >= len(r.Game.Players) {
				r.Game.DealerPos = len(r.Game.Players) - 1
			}
		}
	}

	if r.Status == RoomPlaying && r.Game != nil && r.Game.CountActiveForStore() <= 1 {
		beforeStage := r.Game.Stage
		r.Game.FinishByLastStandingForStore()
		if beforeStage != domain.StageFinished && r.Game.Stage == domain.StageFinished {
			finishedByLeave = true
		}
		r.Status = RoomWaiting
	}

	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	if finishedByLeave {
		m.enqueueAISummaryLocked(r)
	}
	m.enqueueAIDecisionLocked(r)
	m.mu.Unlock()
	return r, nil
}

func chipRefreshEligibleUserIDs(players []RoomPlayer) []string {
	eligible := make([]string, 0, len(players))
	for _, p := range players {
		if !p.IsAI {
			eligible = append(eligible, p.UserID)
		}
	}
	return eligible
}

func containsUserID(userIDs []string, userID string) bool {
	for _, uid := range userIDs {
		if uid == userID {
			return true
		}
	}
	return false
}

func resetRoomPlayerStacks(r *Room, stack int) {
	if r == nil {
		return
	}
	for i := range r.Players {
		r.Players[i].Stack = stack
	}
	if r.Game == nil {
		return
	}
	for _, gp := range r.Game.Players {
		gp.Stack = stack
	}
}

func normalizeChipRefreshVoteDecision(decision string) (ChipRefreshVoteDecision, bool) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case string(ChipRefreshVoteAgree):
		return ChipRefreshVoteAgree, true
	case string(ChipRefreshVoteReject):
		return ChipRefreshVoteReject, true
	default:
		return "", false
	}
}

func canRunChipRefreshVote(r *Room) bool {
	if r == nil {
		return false
	}
	if r.Game == nil {
		return true
	}
	return r.Game.Stage == domain.StageFinished
}

func (m *MemoryStore) StartChipRefreshVote(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		return nil, errors.New("user not in room")
	}
	if r.OwnerUserID != userID {
		return nil, errors.New("only owner can start chip refresh vote")
	}
	if !canRunChipRefreshVote(r) {
		return nil, errors.New("chip refresh vote is only allowed when hand is not in progress")
	}

	eligible := chipRefreshEligibleUserIDs(r.Players)
	if len(eligible) == 0 {
		return nil, errors.New("no eligible players")
	}

	now := time.Now().Unix()
	r.ChipRefreshVote = &ChipRefreshVote{
		StartedByUserID: userID,
		EligibleUserIDs: eligible,
		Votes:           map[string]ChipRefreshVoteDecision{},
		Result:          ChipRefreshVotePending,
		UpdatedAtUnix:   now,
	}
	r.StateVersion++
	r.UpdatedAtUnix = now
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) CastChipRefreshVote(roomID, userID, decision string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		return nil, errors.New("user not in room")
	}
	if !canRunChipRefreshVote(r) {
		return nil, errors.New("chip refresh vote is only allowed when hand is not in progress")
	}

	vote := r.ChipRefreshVote
	if vote == nil || vote.Result != ChipRefreshVotePending {
		return nil, errors.New("no active chip refresh vote")
	}
	if !containsUserID(vote.EligibleUserIDs, userID) {
		return nil, errors.New("only eligible players can vote")
	}

	parsedDecision, ok := normalizeChipRefreshVoteDecision(decision)
	if !ok {
		return nil, errors.New("invalid vote decision")
	}
	if prev, voted := vote.Votes[userID]; voted {
		if prev == parsedDecision {
			return r, nil
		}
		return nil, errors.New("vote already submitted")
	}

	vote.Votes[userID] = parsedDecision
	if parsedDecision == ChipRefreshVoteReject {
		vote.Result = ChipRefreshVoteRejected
	} else {
		allAgreed := true
		for _, uid := range vote.EligibleUserIDs {
			if vote.Votes[uid] != ChipRefreshVoteAgree {
				allAgreed = false
				break
			}
		}
		if allAgreed {
			vote.Result = ChipRefreshVoteApproved
			resetRoomPlayerStacks(r, DefaultPlayerStack)
		}
	}

	now := time.Now().Unix()
	vote.UpdatedAtUnix = now
	r.StateVersion++
	r.UpdatedAtUnix = now
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) NextHand(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("user not in room")
	}
	if r.OwnerUserID != userID {
		m.mu.Unlock()
		return nil, errors.New("only owner can start next hand")
	}
	if r.Game == nil || r.Game.Stage != domain.StageFinished {
		m.mu.Unlock()
		return nil, errors.New("current hand not finished")
	}
	stacks := map[string]int{}
	for _, gp := range r.Game.Players {
		stacks[gp.UserID] = gp.Stack
	}
	for i := range r.Players {
		if v, ok := stacks[r.Players[i].UserID]; ok {
			r.Players[i].Stack = v
		}
	}
	g, err := m.buildGameFromRoom(r, stacks)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	r.Game = g
	r.Status = RoomPlaying
	r.ActionSeen = map[string]bool{}
	r.ChipRefreshVote = nil
	r.HandCounter++
	if len(r.Players) > 0 {
		r.NextDealerPos = nextDealerPosAfterStart(r, g)
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	m.enqueueAIDecisionLocked(r)
	m.mu.Unlock()
	return r, nil
}

func (m *MemoryStore) ApplyAction(roomID, userID, actionID, action string, amount int, expectedVersion int64) (*Room, error) {
	return m.applyAction(roomID, userID, actionID, action, amount, expectedVersion, false)
}

func (m *MemoryStore) applyAction(roomID, userID, actionID, action string, amount int, expectedVersion int64, allowAIManaged bool) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		m.mu.Unlock()
		return nil, errors.New("user not in room")
	}
	if r.Game == nil || r.Status != RoomPlaying {
		m.mu.Unlock()
		return nil, errors.New("game not started")
	}
	if expectedVersion != r.StateVersion {
		m.mu.Unlock()
		return nil, errors.New("version conflict")
	}
	if actionID != "" && r.ActionSeen[actionID] {
		m.mu.Unlock()
		return r, nil
	}
	for _, gp := range r.Game.Players {
		if gp.UserID == userID {
			if gp.AIManaged && !allowAIManaged {
				m.mu.Unlock()
				return nil, errors.New("player is ai-managed")
			}
			break
		}
	}
	if err := r.Game.ApplyAction(userID, action, amount); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if actionID != "" {
		r.ActionSeen[actionID] = true
	}
	for i := range r.Players {
		for _, gp := range r.Game.Players {
			if r.Players[i].UserID == gp.UserID {
				r.Players[i].Stack = gp.Stack
				break
			}
		}
	}
	finishedNow := r.Game.Stage == domain.StageFinished
	if finishedNow {
		r.Status = RoomWaiting
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	if finishedNow {
		m.enqueueAISummaryLocked(r)
	}
	m.enqueueAIDecisionLocked(r)
	m.mu.Unlock()
	return r, nil
}

func (m *MemoryStore) applyActionFromAI(task *aiDecisionTask, decision ai.Decision) {
	m.mu.Lock()
	delete(m.aiWorkers, task.RoomID)
	m.mu.Unlock()

	_, err := m.applyAction(task.RoomID, task.AIUserID, task.ActionID, decision.Action, decision.Amount, task.ExpectedVersion, true)
	if err == nil {
		return
	}
	if task.RetriesLeft <= 0 {
		return
	}
	m.mu.Lock()
	room, ok := m.rooms[task.RoomID]
	if ok {
		m.enqueueAIDecisionLockedWithRetry(room, task.RetriesLeft-1)
	}
	m.mu.Unlock()
}

func (m *MemoryStore) ApplyReveal(roomID, userID, actionID string, mask int, expectedVersion int64) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		return nil, errors.New("spectator is read-only")
	}
	if !isPlayer(r, userID) {
		return nil, errors.New("user not in room")
	}
	if r.Game == nil {
		return nil, errors.New("game not started")
	}
	if expectedVersion != r.StateVersion {
		return nil, errors.New("version conflict")
	}
	if actionID != "" && r.ActionSeen[actionID] {
		return r, nil
	}
	if err := r.Game.SetRevealSelection(userID, mask); err != nil {
		return nil, err
	}
	if actionID != "" {
		r.ActionSeen[actionID] = true
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func normalizePhraseID(phraseID string) string {
	return strings.TrimSpace(strings.ToLower(phraseID))
}

func isQuickChatPhraseAllowed(phraseID string) bool {
	_, ok := quickChatPhrases[phraseID]
	return ok
}

func (m *MemoryStore) userInRoom(r *Room, userID string) (RoomPlayer, bool) {
	for _, p := range r.Players {
		if p.UserID == userID {
			return p, true
		}
	}
	return RoomPlayer{}, false
}

func cleanupQuickChats(room *Room, nowMs int64) {
	minAlive := nowMs - QuickChatRetentionMS
	filtered := make([]QuickChatEvent, 0, len(room.QuickChats))
	for _, ev := range room.QuickChats {
		if ev.CreatedAtMs >= minAlive {
			filtered = append(filtered, ev)
		}
	}
	room.QuickChats = filtered

	if len(room.QuickChatSeenOrder) == 0 {
		return
	}
	retained := room.QuickChatSeenOrder[:0]
	for _, item := range room.QuickChatSeenOrder {
		if item.CreatedAtMs >= minAlive {
			retained = append(retained, item)
			continue
		}
		delete(room.QuickChatSeen, item.ActionID)
	}
	room.QuickChatSeenOrder = retained
}

func (m *MemoryStore) SendQuickChat(roomID, userID, actionID, phraseID string) (*Room, *QuickChatEvent, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[roomID]
	if !ok {
		return nil, nil, 0, errors.New("room not found")
	}
	if isSpectator(r, userID) {
		return nil, nil, 0, errors.New("spectator is read-only")
	}
	roomPlayer, ok := m.userInRoom(r, userID)
	if !ok {
		return nil, nil, 0, errors.New("user not in room")
	}

	nowMs := time.Now().UnixMilli()
	cleanupQuickChats(r, nowMs)

	normalizedActionID := strings.TrimSpace(actionID)
	if normalizedActionID != "" && r.QuickChatSeen[normalizedActionID] {
		return r, nil, 0, nil
	}

	normalizedPhrase := normalizePhraseID(phraseID)
	if !isQuickChatPhraseAllowed(normalizedPhrase) {
		return nil, nil, 0, errors.New("invalid phrase")
	}

	lastSent := r.QuickChatLastSentAt[userID]
	if lastSent > 0 {
		delta := nowMs - lastSent
		if delta < QuickChatCooldownMS {
			return nil, nil, QuickChatCooldownMS - delta, errors.New("quick chat cooldown")
		}
	}

	r.QuickChatNextEventID++
	event := QuickChatEvent{
		EventID:     r.QuickChatNextEventID,
		UserID:      userID,
		Username:    roomPlayer.Username,
		PhraseID:    normalizedPhrase,
		CreatedAtMs: nowMs,
		ExpireAtMs:  nowMs + QuickChatBubbleTTLMS,
	}
	r.QuickChats = append(r.QuickChats, event)
	r.QuickChatLastSentAt[userID] = nowMs
	if normalizedActionID != "" {
		r.QuickChatSeen[normalizedActionID] = true
		r.QuickChatSeenOrder = append(r.QuickChatSeenOrder, quickChatSeenKey{ActionID: normalizedActionID, CreatedAtMs: nowMs})
	}
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++

	return r, &event, 0, nil
}

func (m *MemoryStore) QuickChatPhrases() []string {
	phrases := make([]string, len(quickChatPhraseList))
	copy(phrases, quickChatPhraseList)
	return phrases
}

func (m *MemoryStore) QuickChatConfig() (int64, int64, int64) {
	return QuickChatBubbleTTLMS, QuickChatCooldownMS, QuickChatRetentionMS
}

func (m *MemoryStore) ListQuickChats(roomID string, sinceEventID int64) (*Room, []QuickChatEvent, int64, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.rooms[roomID]
	if !ok {
		return nil, nil, 0, 0, errors.New("room not found")
	}

	nowMs := time.Now().UnixMilli()
	cleanupQuickChats(r, nowMs)

	result := make([]QuickChatEvent, 0, len(r.QuickChats))
	latestEventID := int64(0)
	for _, ev := range r.QuickChats {
		if ev.EventID > latestEventID {
			latestEventID = ev.EventID
		}
		if ev.EventID > sinceEventID {
			result = append(result, ev)
		}
	}

	return r, result, latestEventID, nowMs, nil
}

func (m *MemoryStore) TouchUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastActive[userID] = time.Now().Unix()
}

func (m *MemoryStore) LeaveAllRooms(userID string) {
	m.mu.Lock()
	var roomIDs []string
	for rid, r := range m.rooms {
		if isMember(r, userID) {
			roomIDs = append(roomIDs, rid)
		}
	}
	m.mu.Unlock()

	for _, rid := range roomIDs {
		_, _ = m.LeaveRoom(rid, userID)
	}
}

func (m *MemoryStore) RemoveUser(userID string) {
	m.LeaveAllRooms(userID)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, userID)
	delete(m.lastActive, userID)
}

const idleTimeout = 60 * 60

func (m *MemoryStore) idleCleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		now := time.Now().Unix()
		m.mu.RLock()
		var expired []string
		for uid, last := range m.lastActive {
			if now-last >= idleTimeout {
				expired = append(expired, uid)
			}
		}
		m.mu.RUnlock()

		for _, uid := range expired {
			m.RemoveUser(uid)
		}
	}
}

func countHumans(players []RoomPlayer) int {
	count := 0
	for _, p := range players {
		if !p.IsAI {
			count++
		}
	}
	return count
}

func firstHumanOwner(players []RoomPlayer) string {
	for _, p := range players {
		if !p.IsAI {
			return p.UserID
		}
	}
	return ""
}

func playerIndex(r *Room, userID string) int {
	for i, p := range r.Players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}

func spectatorIndex(r *Room, userID string) int {
	for i, s := range r.Spectators {
		if s.UserID == userID {
			return i
		}
	}
	return -1
}

func isPlayer(r *Room, userID string) bool {
	return playerIndex(r, userID) >= 0
}

func isSpectator(r *Room, userID string) bool {
	return spectatorIndex(r, userID) >= 0
}

func isMember(r *Room, userID string) bool {
	return isPlayer(r, userID) || isSpectator(r, userID)
}

func cardToText(card domain.Card) string {
	suits := []string{"C", "D", "H", "S"}
	rank := card.Rank
	r := fmt.Sprintf("%d", rank)
	switch rank {
	case 11:
		r = "J"
	case 12:
		r = "Q"
	case 13:
		r = "K"
	case 14:
		r = "A"
	}
	s := "?"
	suitIdx := int(card.Suit)
	if suitIdx >= 0 && suitIdx < len(suits) {
		s = suits[suitIdx]
	}
	return r + s
}

func parseCardText(card string) (domain.Card, bool) {
	rank, suit, ok := parseCardTextRankSuit(card)
	if !ok {
		return domain.Card{}, false
	}
	var s domain.Suit
	switch suit {
	case 'C':
		s = domain.Clubs
	case 'D':
		s = domain.Diamonds
	case 'H':
		s = domain.Hearts
	case 'S':
		s = domain.Spades
	default:
		return domain.Card{}, false
	}
	return domain.Card{Rank: rank, Suit: s}, true
}

func preflopTierFromHoleCards(hole []domain.Card) string {
	if len(hole) < 2 {
		return "unknown"
	}
	first := hole[0]
	second := hole[1]
	high := first.Rank
	low := second.Rank
	if low > high {
		high, low = low, high
	}
	isPair := first.Rank == second.Rank
	isSuited := first.Suit == second.Suit
	gap := high - low
	if isPair {
		switch {
		case high >= 12:
			return "premium"
		case high >= 9:
			return "strong"
		case high >= 6:
			return "playable"
		default:
			return "speculative"
		}
	}
	if high == 14 && low >= 10 {
		if isSuited {
			return "premium"
		}
		return "strong"
	}
	if isSuited && gap <= 1 && high >= 10 {
		return "strong"
	}
	if (high >= 13 && low >= 10) || (isSuited && gap <= 2 && high >= 9) {
		return "playable"
	}
	if isSuited || gap <= 2 || high >= 12 {
		return "speculative"
	}
	return "trash"
}

func preflopActiveOrder(game *domain.GameState) []int {
	if game == nil || len(game.Players) == 0 {
		return nil
	}
	start := (game.BigBlindPos + 1) % len(game.Players)
	order := make([]int, 0, len(game.Players))
	for i := 0; i < len(game.Players); i++ {
		pos := (start + i) % len(game.Players)
		p := game.Players[pos]
		if p == nil || p.Folded {
			continue
		}
		order = append(order, pos)
	}
	return order
}

func preflopPositionForPlayer(game *domain.GameState, heroPos int) string {
	if game == nil || heroPos < 0 || heroPos >= len(game.Players) {
		return "unknown"
	}
	order := preflopActiveOrder(game)
	if len(order) == 0 {
		return "unknown"
	}
	activeCount := len(order)
	if heroPos == game.BigBlindPos {
		return "bb"
	}
	if heroPos == game.SmallBlindPos {
		if activeCount == 2 {
			return "btn_sb"
		}
		return "sb"
	}
	if heroPos == game.DealerPos {
		if activeCount == 2 {
			return "btn_sb"
		}
		return "btn"
	}
	heroIdx := -1
	for i, pos := range order {
		if pos == heroPos {
			heroIdx = i
			break
		}
	}
	if heroIdx < 0 {
		return "unknown"
	}
	if activeCount <= 4 {
		return "utg"
	}
	if activeCount == 5 {
		if heroIdx == 1 {
			return "co"
		}
		return "utg"
	}
	btnIdx := activeCount - 3
	coIdx := btnIdx - 1
	hjIdx := btnIdx - 2
	switch {
	case heroIdx == 0:
		return "utg"
	case heroIdx == coIdx:
		return "co"
	case hjIdx >= 1 && heroIdx == hjIdx:
		return "hj"
	default:
		return "mp"
	}
}

func effectiveStackBBForPlayer(game *domain.GameState, heroPos int) float64 {
	if game == nil || heroPos < 0 || heroPos >= len(game.Players) {
		return 0
	}
	bb := maxInt(1, game.OpenBetMin)
	hero := game.Players[heroPos]
	if hero == nil || hero.Folded {
		return 0
	}
	effective := hero.Stack
	if effective <= 0 {
		return 0
	}
	for i, p := range game.Players {
		if i == heroPos || p == nil || p.Folded || p.Stack <= 0 {
			continue
		}
		if p.Stack < effective {
			effective = p.Stack
		}
	}
	return clampFloat(float64(effective)/float64(bb), 0, 400)
}

func preflopRaiseLevel(input ai.DecisionInput) float64 {
	bb := maxInt(1, input.OpenBetMin)
	roundBet := maxInt(input.RoundBet, bb)
	return float64(roundBet) / float64(bb)
}

func preflopPositionTightness(position string) float64 {
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "utg":
		return 0.10
	case "mp":
		return 0.06
	case "hj":
		return 0.03
	case "co":
		return -0.02
	case "btn", "btn_sb":
		return -0.06
	case "sb":
		return 0.01
	case "bb":
		return -0.03
	default:
		return 0.04
	}
}

func preflopHoleStrengthCards(hole []domain.Card) (float64, bool, bool, int, int) {
	score := preflopTierScore(preflopTierFromHoleCards(hole))
	if len(hole) < 2 {
		return clampFloat(score, 0.02, 0.98), false, false, 0, 10
	}
	first := hole[0]
	second := hole[1]
	high := first.Rank
	low := second.Rank
	if low > high {
		high, low = low, high
	}
	gap := high - low
	isPair := first.Rank == second.Rank
	isSuited := first.Suit == second.Suit
	broadwayCount := 0
	if high >= 10 {
		broadwayCount++
	}
	if low >= 10 {
		broadwayCount++
	}

	if isPair {
		score += 0.12 + clampFloat(float64(high-7), -5, 7)*0.018
		switch {
		case high >= 12:
			score += 0.08
		case high >= 10:
			score += 0.04
		case high <= 4:
			score -= 0.02
		}
	} else {
		score += float64(broadwayCount) * 0.02
		if high == 14 && low >= 10 {
			score += 0.07
		} else if high == 14 {
			score += 0.03
		}
		if isSuited {
			score += 0.035
		}
		switch {
		case gap <= 1:
			score += 0.035
		case gap == 2:
			score += 0.018
		case gap >= 4:
			score -= 0.055
		}
		if low <= 5 && high == 14 && isSuited {
			score += 0.025
		}
		if low >= 10 && gap <= 2 {
			score += 0.02
		}
		if high <= 9 && gap >= 5 && !isSuited {
			score -= 0.035
		}
	}
	return clampFloat(score, 0.02, 0.98), isPair, isSuited, high, gap
}

func preflopHandScore(input ai.DecisionInput) (float64, bool, bool, int, int) {
	if len(input.HoleCards) < 2 {
		return clampFloat(preflopTierScore(input.PreflopTier), 0.02, 0.98), false, false, 0, 10
	}
	hole := make([]domain.Card, 0, 2)
	for _, raw := range input.HoleCards[:2] {
		card, ok := parseCardText(raw)
		if !ok {
			return clampFloat(preflopTierScore(input.PreflopTier), 0.02, 0.98), false, false, 0, 10
		}
		hole = append(hole, card)
	}
	return preflopHoleStrengthCards(hole)
}

func madeHandStrengthFromCategory(category int) string {
	switch {
	case category >= 6:
		return "monster"
	case category >= 4:
		return "strong"
	case category >= 2:
		return "medium"
	case category == 1:
		return "weak"
	default:
		return "none"
	}
}

func hasFlushDraw(hole []domain.Card, board []domain.Card) bool {
	all := append(append([]domain.Card{}, hole...), board...)
	if len(all) < 4 {
		return false
	}
	suitCount := map[domain.Suit]int{}
	for _, c := range all {
		suitCount[c.Suit]++
	}
	for _, count := range suitCount {
		if count >= 4 {
			return true
		}
	}
	return false
}

func hasOpenEndedStraightDraw(hole []domain.Card, board []domain.Card) bool {
	all := append(append([]domain.Card{}, hole...), board...)
	if len(all) < 4 {
		return false
	}
	rankMap := map[int]bool{}
	for _, c := range all {
		rankMap[c.Rank] = true
		if c.Rank == 14 {
			rankMap[1] = true
		}
	}
	ranks := make([]int, 0, len(rankMap))
	for rank := range rankMap {
		ranks = append(ranks, rank)
	}
	sort.Ints(ranks)
	if len(ranks) < 4 {
		return false
	}
	for i := 0; i <= len(ranks)-4; i++ {
		window := ranks[i : i+4]
		if window[3]-window[0] != 3 {
			continue
		}
		if window[1] != window[0]+1 || window[2] != window[1]+1 || window[3] != window[2]+1 {
			continue
		}
		if rankMap[window[0]-1] || rankMap[window[3]+1] {
			return true
		}
	}
	return false
}

func hasGutshotStraightDraw(hole []domain.Card, board []domain.Card) bool {
	all := append(append([]domain.Card{}, hole...), board...)
	if len(all) < 4 {
		return false
	}
	rankMap := map[int]bool{}
	for _, c := range all {
		rankMap[c.Rank] = true
		if c.Rank == 14 {
			rankMap[1] = true
		}
	}
	for high := 5; high <= 14; high++ {
		present := 0
		missing := 0
		for rank := high - 4; rank <= high; rank++ {
			if rankMap[rank] {
				present++
			} else {
				missing++
			}
		}
		if present == 4 && missing == 1 {
			if (rankMap[high-4] && rankMap[high-3] && rankMap[high-2] && rankMap[high-1]) ||
				(rankMap[high-3] && rankMap[high-2] && rankMap[high-1] && rankMap[high]) {
				continue
			}
			return true
		}
	}
	return false
}

func buildHandStrengthFeatures(hole []domain.Card, board []domain.Card) (string, int, []int, string, string, []string) {
	category := ""
	categoryRank := -1
	ranks := []int{}
	madeStrength := "none"
	draws := []string{}
	if len(hole)+len(board) >= 5 {
		cards := append(append([]domain.Card{}, board...), hole...)
		best, _, name := domain.BestOfSeven(cards)
		category = name
		categoryRank = best.Category
		ranks = append([]int(nil), best.Ranks...)
		madeStrength = madeHandStrengthFromCategory(best.Category)
	}
	if hasFlushDraw(hole, board) {
		draws = append(draws, "flush_draw")
	}
	if hasOpenEndedStraightDraw(hole, board) {
		draws = append(draws, "open_ended_straight_draw")
	} else if hasGutshotStraightDraw(hole, board) {
		draws = append(draws, "gutshot")
	}
	if len(draws) == 0 {
		draws = []string{"none"}
	}
	return category, categoryRank, ranks, preflopTierFromHoleCards(hole), madeStrength, draws
}

func cloneProfiles(mem map[string]*OpponentProfile) map[string]ai.Profile {
	out := map[string]ai.Profile{}
	for uid, profile := range mem {
		if profile == nil {
			continue
		}
		tend := make([]string, len(profile.Tendencies))
		copy(tend, profile.Tendencies)
		out[uid] = ai.Profile{Style: profile.Style, Tendencies: tend, Advice: profile.Advice}
	}
	return out
}

func opponentStatsSnapshot(stats *OpponentStat) ai.OpponentStats {
	if stats == nil {
		return ai.OpponentStats{}
	}
	vpip := 0.0
	pfr := 0.0
	showdownRate := 0.0
	showdownWinRate := 0.0
	if stats.Hands > 0 {
		denom := float64(stats.Hands)
		vpip = float64(stats.VPIPHands) / denom
		pfr = float64(stats.PFRHands) / denom
		showdownRate = float64(stats.WentToShowdownHands) / denom
	}
	aggressionFactor := float64(stats.PostflopAggActions)
	if stats.PostflopCallActions > 0 {
		aggressionFactor = aggressionFactor / float64(stats.PostflopCallActions)
	}
	if aggressionFactor > 8 {
		aggressionFactor = 8
	}
	foldRate := 0.0
	if stats.DecisionActions > 0 {
		foldRate = float64(stats.FoldActions) / float64(stats.DecisionActions)
	}
	if stats.WentToShowdownHands > 0 {
		showdownWinRate = float64(stats.WonShowdownHands) / float64(stats.WentToShowdownHands)
	}
	return ai.OpponentStats{
		Hands:            stats.Hands,
		VPIP:             clampFloat(vpip, 0, 1),
		PFR:              clampFloat(pfr, 0, 1),
		AggressionFactor: clampFloat(aggressionFactor, 0, 8),
		FoldRate:         clampFloat(foldRate, 0, 1),
		ShowdownRate:     clampFloat(showdownRate, 0, 1),
		ShowdownWinRate:  clampFloat(showdownWinRate, 0, 1),
	}
}

func cloneOpponentStats(mem map[string]*OpponentStat) map[string]ai.OpponentStats {
	out := map[string]ai.OpponentStats{}
	for uid, stats := range mem {
		if stats == nil {
			continue
		}
		out[uid] = opponentStatsSnapshot(stats)
	}
	return out
}

func (m *MemoryStore) ensureAIMemory(room *Room, aiUserID string) *RoomAIMemory {
	if room.AIMemory == nil {
		room.AIMemory = map[string]*RoomAIMemory{}
	}
	if room.AIMemory[aiUserID] == nil {
		room.AIMemory[aiUserID] = &RoomAIMemory{
			HandSummaries:    []string{},
			OpponentProfiles: map[string]*OpponentProfile{},
			OpponentStats:    map[string]*OpponentStat{},
		}
	}
	if room.AIMemory[aiUserID].OpponentProfiles == nil {
		room.AIMemory[aiUserID].OpponentProfiles = map[string]*OpponentProfile{}
	}
	if room.AIMemory[aiUserID].OpponentStats == nil {
		room.AIMemory[aiUserID].OpponentStats = map[string]*OpponentStat{}
	}
	return room.AIMemory[aiUserID]
}

func shouldSummarizeAIMemoryForPlayer(room *Room, player *domain.GamePlayer, handID int64) bool {
	if room == nil || player == nil {
		return false
	}
	if player.IsAI {
		return true
	}
	if room.AIMemory == nil {
		return false
	}
	mem := room.AIMemory[player.UserID]
	if mem == nil {
		return false
	}
	return mem.LastDecisionHand == handID
}

func clampInt(value int, low int, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value float64, low float64, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func activeOpponentCount(input ai.DecisionInput) int {
	count := 0
	for _, p := range input.Players {
		if p.UserID == input.AIUserID || p.Folded {
			continue
		}
		count++
	}
	if count <= 0 {
		return 1
	}
	return count
}

func hasDrawPotential(flags []string) (strong bool, weak bool) {
	for _, raw := range flags {
		flag := strings.ToLower(strings.TrimSpace(raw))
		switch flag {
		case "flush_draw", "open_ended_straight_draw":
			strong = true
		case "gutshot":
			weak = true
		}
	}
	return strong, weak
}

func drawStageWeight(stage string) float64 {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "flop":
		return 1.0
	case "turn":
		return 0.62
	case "river":
		return 0.12
	default:
		return 0.45
	}
}

func preflopTierScore(raw string) float64 {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "premium":
		return 0.82
	case "strong":
		return 0.68
	case "playable":
		return 0.56
	case "speculative":
		return 0.44
	case "trash":
		return 0.29
	default:
		return 0.45
	}
}

func madeStrengthScore(raw string) float64 {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "monster":
		return 0.91
	case "strong":
		return 0.76
	case "medium":
		return 0.58
	case "weak":
		return 0.43
	default:
		return 0.27
	}
}

func estimateFallbackEquity(input ai.DecisionInput) float64 {
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	heuristic := madeStrengthScore(input.MadeHandStrength)
	if stage == "preflop" {
		heuristic = preflopTierScore(input.PreflopTier)
	} else if input.HandCategoryRank >= 0 {
		if input.HandCategoryRank >= 6 {
			heuristic += 0.08
		} else if input.HandCategoryRank >= 4 {
			heuristic += 0.04
		}
	}
	strongDraw, weakDraw := hasDrawPotential(input.DrawFlags)
	drawWeight := drawStageWeight(stage)
	if strongDraw {
		heuristic += 0.17 * drawWeight
	} else if weakDraw {
		heuristic += 0.08 * drawWeight
	}
	if stage == "river" && strings.EqualFold(strings.TrimSpace(input.HandCategory), "high_card") {
		heuristic -= 0.07
	}
	heuristic = clampFloat(heuristic, 0.05, 0.97)

	monteCarlo, ok := estimateMonteCarloEquity(input)
	if !ok {
		return heuristic
	}
	weight := 0.72
	switch stage {
	case "preflop":
		weight = 0.82
	case "flop":
		weight = 0.80
	case "turn":
		weight = 0.78
	case "river":
		weight = 0.90
	}
	blended := monteCarlo*weight + heuristic*(1.0-weight)
	return clampFloat(blended, 0.03, 0.99)
}

func estimateFallbackPressure(input ai.DecisionInput) float64 {
	if input.CallAmount <= 0 {
		return 0
	}
	pot := float64(maxInt(1, input.Pot))
	stack := float64(maxInt(1, input.Stack))
	call := float64(input.CallAmount)
	potPressure := call / (pot + call)
	stackPressure := call / stack
	pressure := potPressure*0.60 + stackPressure*0.90
	if input.CallAmount > input.Stack/2 {
		pressure += 0.18
	}
	if input.CallAmount > input.Pot {
		pressure += 0.10
	}
	return clampFloat(pressure, 0, 1.5)
}

func profileStrategyAdjustments(profiles map[string]ai.Profile) (float64, float64, float64) {
	if len(profiles) == 0 {
		return 0, 0, 0
	}
	var foldEqAdj float64
	var valueAdj float64
	var trapAdj float64
	for _, p := range profiles {
		text := strings.ToLower(strings.TrimSpace(p.Style + " " + p.Advice + " " + strings.Join(p.Tendencies, " ")))
		if text == "" {
			continue
		}
		if containsAny(text,
			"tight", "nit", "passive", "tight-passive", "nitty",
			"紧", "紧手", "被动", "保守", "谨慎", "偏紧",
		) {
			foldEqAdj += 0.03
		}
		if containsAny(text,
			"fold", "conservative", "overfold",
			"弃牌", "爱弃牌", "弃得多", "过度弃牌", "保守",
		) {
			foldEqAdj += 0.02
		}
		if containsAny(text,
			"loose", "calling", "station", "calling-station", "loose-passive",
			"松", "松手", "跟注站", "爱跟", "跟得多", "粘", "宽",
		) {
			foldEqAdj -= 0.04
			valueAdj += 0.05
		}
		if containsAny(text,
			"aggressive", "raise", "bluff", "loose-aggressive", "tight-aggressive", "maniac",
			"激进", "好斗", "爱加注", "加注多", "诈唬", "偷鸡", "凶",
		) {
			foldEqAdj -= 0.01
			trapAdj += 0.04
		}
		if containsAny(text,
			"trap", "tricky", "slowplay",
			"慢打", "设套", "诱捕", "埋伏", "诡", "狡猾",
		) {
			trapAdj += 0.05
		}
	}
	return clampFloat(foldEqAdj, -0.15, 0.15), clampFloat(valueAdj, -0.05, 0.18), clampFloat(trapAdj, 0, 0.22)
}

func profileRangeBias(profile ai.Profile) (float64, float64, float64, float64) {
	text := strings.ToLower(strings.TrimSpace(profile.Style + " " + profile.Advice + " " + strings.Join(profile.Tendencies, " ")))
	if text == "" {
		return 0, 0, 0, 0
	}
	tightBias := 0.0
	looseBias := 0.0
	aggressionBias := 0.0
	trapBias := 0.0
	if containsAny(text,
		"tight", "nit", "passive", "tight-passive", "nitty",
		"紧", "紧手", "偏紧", "保守", "被动",
	) {
		tightBias += 0.22
	}
	if containsAny(text,
		"loose", "calling", "station", "calling-station", "loose-passive",
		"松", "松手", "宽", "跟注站", "爱跟", "跟得多", "粘",
	) {
		looseBias += 0.24
	}
	if containsAny(text,
		"aggressive", "raise", "bluff", "loose-aggressive", "tight-aggressive", "maniac",
		"激进", "好斗", "爱加注", "加注多", "诈唬", "偷鸡", "凶",
	) {
		aggressionBias += 0.22
	}
	if containsAny(text,
		"trap", "tricky", "slowplay",
		"慢打", "设套", "诱捕", "埋伏", "狡猾",
	) {
		trapBias += 0.18
	}
	return clampFloat(tightBias, 0, 0.32), clampFloat(looseBias, 0, 0.34), clampFloat(aggressionBias, 0, 0.30), clampFloat(trapBias, 0, 0.24)
}

func opponentStatsStrategyAdjustments(stats map[string]ai.OpponentStats) (float64, float64, float64) {
	if len(stats) == 0 {
		return 0, 0, 0
	}
	var foldEqAdj float64
	var valueAdj float64
	var trapAdj float64
	for _, s := range stats {
		if s.Hands < 3 {
			continue
		}
		weight := clampFloat(float64(s.Hands)/15.0, 0.25, 1.0)
		if s.VPIP >= 0.40 && s.PFR <= 0.18 {
			foldEqAdj -= 0.04 * weight
			valueAdj += 0.06 * weight
		}
		if s.VPIP <= 0.22 {
			foldEqAdj += 0.04 * weight
		}
		if s.FoldRate >= 0.38 {
			foldEqAdj += 0.05 * weight
		}
		if s.FoldRate <= 0.20 {
			foldEqAdj -= 0.05 * weight
			valueAdj += 0.04 * weight
		}
		if s.PFR >= 0.26 && s.AggressionFactor >= 2.2 {
			trapAdj += 0.05 * weight
			foldEqAdj -= 0.02 * weight
		}
		if s.ShowdownRate >= 0.34 && s.ShowdownWinRate <= 0.47 {
			valueAdj += 0.03 * weight
		}
	}
	return clampFloat(foldEqAdj, -0.2, 0.2), clampFloat(valueAdj, -0.08, 0.22), clampFloat(trapAdj, 0, 0.26)
}

func tableAggressionScore(logs []ai.ActionLog, stage string) float64 {
	if len(logs) == 0 {
		return 0.45
	}
	stage = strings.ToLower(strings.TrimSpace(stage))
	start := 0
	if len(logs) > 14 {
		start = len(logs) - 14
	}
	var score float64
	var count float64
	for i := start; i < len(logs); i++ {
		log := logs[i]
		weight := 1.0
		if stage != "" && stage != strings.ToLower(strings.TrimSpace(log.Stage)) {
			weight = 0.6
		}
		switch strings.ToLower(strings.TrimSpace(log.Action)) {
		case "bet", "allin":
			score += 1.0 * weight
		case "call":
			score += 0.35 * weight
		case "check":
			score += 0.15 * weight
		case "fold":
			score += 0.05 * weight
		}
		count += weight
	}
	if count <= 0 {
		return 0.45
	}
	return clampFloat(score/count, 0.05, 0.95)
}

func parseCardTextRankSuit(card string) (int, byte, bool) {
	value := strings.ToUpper(strings.TrimSpace(card))
	if len(value) < 2 {
		return 0, 0, false
	}
	suit := value[len(value)-1]
	rankText := value[:len(value)-1]
	switch rankText {
	case "A":
		return 14, suit, true
	case "K":
		return 13, suit, true
	case "Q":
		return 12, suit, true
	case "J":
		return 11, suit, true
	case "T":
		return 10, suit, true
	}
	var rank int
	_, err := fmt.Sscanf(rankText, "%d", &rank)
	if err != nil || rank < 2 || rank > 14 {
		return 0, 0, false
	}
	return rank, suit, true
}

type visibleActionSummary struct {
	PreflopCalls       int
	PreflopRaises      int
	CurrentStageCalls  int
	CurrentStageAgg    int
	CurrentStageChecks int
	TotalAgg           int
}

func summarizeVisibleActionsByUser(logs []ai.ActionLog, stage string) map[string]visibleActionSummary {
	stage = strings.ToLower(strings.TrimSpace(stage))
	out := map[string]visibleActionSummary{}
	for _, log := range logs {
		if strings.TrimSpace(log.UserID) == "" {
			continue
		}
		summary := out[log.UserID]
		logStage := strings.ToLower(strings.TrimSpace(log.Stage))
		action := strings.ToLower(strings.TrimSpace(log.Action))
		switch action {
		case "bet", "allin":
			summary.TotalAgg++
			if logStage == "preflop" {
				summary.PreflopRaises++
			}
			if logStage == stage {
				summary.CurrentStageAgg++
			}
		case "call":
			if logStage == "preflop" {
				summary.PreflopCalls++
			}
			if logStage == stage {
				summary.CurrentStageCalls++
			}
		case "check":
			if logStage == stage {
				summary.CurrentStageChecks++
			}
		}
		out[log.UserID] = summary
	}
	return out
}

func previousStageName(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "river":
		return "turn"
	case "turn":
		return "flop"
	case "flop":
		return "preflop"
	default:
		return ""
	}
}

func postflopStagesBefore(stage string) []string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case "turn":
		return []string{"flop"}
	case "river":
		return []string{"flop", "turn"}
	default:
		return nil
	}
}

func heroPostflopBarrelCount(input ai.DecisionInput) int {
	count := 0
	for _, stage := range postflopStagesBefore(input.Stage) {
		if lastAggressorForStage(input.RecentActionLog, stage) == input.AIUserID {
			count++
		}
	}
	return count
}

func previousStreetHeroBarrelCalled(input ai.DecisionInput) bool {
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	previous := previousStageName(stage)
	if previous == "" || previous == "preflop" {
		return false
	}
	if lastAggressorForStage(input.RecentActionLog, previous) != input.AIUserID {
		return false
	}
	summary := summarizeVisibleActionsByUser(input.RecentActionLog, previous)
	sawCall := false
	for _, player := range input.Players {
		if player.UserID == input.AIUserID || player.Folded || player.AllIn {
			continue
		}
		opp := summary[player.UserID]
		if opp.CurrentStageAgg > 0 {
			return false
		}
		if opp.CurrentStageCalls > 0 {
			sawCall = true
		}
	}
	return sawCall
}

func visibleRangeCapScore(input ai.DecisionInput) float64 {
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	if stage == "" || stage == "preflop" {
		return 0
	}
	currentSummary := summarizeVisibleActionsByUser(input.RecentActionLog, stage)
	previous := previousStageName(stage)
	prevSummary := map[string]visibleActionSummary{}
	heroAggressedPrev := false
	if previous != "" {
		prevSummary = summarizeVisibleActionsByUser(input.RecentActionLog, previous)
		heroAggressedPrev = lastAggressorForStage(input.RecentActionLog, previous) == input.AIUserID
	}
	currentAggressor := lastAggressorForStage(input.RecentActionLog, stage)
	score := 0.0
	activeOpponents := 0
	for _, player := range input.Players {
		if player.UserID == input.AIUserID || player.Folded || player.AllIn {
			continue
		}
		activeOpponents++
		oppScore := 0.0
		current := currentSummary[player.UserID]
		if currentAggressor == "" {
			if current.CurrentStageChecks > 0 {
				oppScore += 0.07
			}
			if current.CurrentStageCalls > 0 && current.CurrentStageAgg == 0 {
				oppScore += 0.03
			}
		} else if currentAggressor == input.AIUserID && current.CurrentStageCalls > 0 && current.CurrentStageAgg == 0 {
			oppScore += 0.05
		}
		if heroAggressedPrev {
			prev := prevSummary[player.UserID]
			if prev.CurrentStageCalls > 0 && prev.CurrentStageAgg == 0 {
				oppScore += 0.05
			}
		}
		stats := input.OpponentStats[player.UserID]
		if stats.Hands >= 3 {
			if stats.AggressionFactor <= 1.2 {
				oppScore += 0.02
			}
			if stats.VPIP >= 0.34 && stats.PFR <= 0.16 {
				oppScore += 0.02
			}
		}
		_, looseBias, aggressionBias, _ := profileRangeBias(input.Profiles[player.UserID])
		if looseBias > 0.12 && aggressionBias < 0.10 {
			oppScore += 0.02
		}
		if currentAggressor == "" && strings.EqualFold(strings.TrimSpace(player.LastAction), "check") {
			oppScore += 0.03
		}
		score += oppScore
	}
	if activeOpponents == 0 {
		return 0
	}
	if activeOpponents >= 3 {
		score *= 0.78
	}
	return clampFloat(score, 0, 0.26)
}

func stageActionCount(logs []ai.ActionLog, stage string) int {
	stage = strings.ToLower(strings.TrimSpace(stage))
	count := 0
	for _, log := range logs {
		if stage == strings.ToLower(strings.TrimSpace(log.Stage)) {
			count++
		}
	}
	return count
}

func lastActorForStage(logs []ai.ActionLog, stage string) string {
	stage = strings.ToLower(strings.TrimSpace(stage))
	for i := len(logs) - 1; i >= 0; i-- {
		if stage == strings.ToLower(strings.TrimSpace(logs[i].Stage)) {
			return logs[i].UserID
		}
	}
	return ""
}

func lastAggressorForStage(logs []ai.ActionLog, stage string) string {
	stage = strings.ToLower(strings.TrimSpace(stage))
	for i := len(logs) - 1; i >= 0; i-- {
		if stage != strings.ToLower(strings.TrimSpace(logs[i].Stage)) {
			continue
		}
		action := strings.ToLower(strings.TrimSpace(logs[i].Action))
		if action == "bet" || action == "allin" {
			return logs[i].UserID
		}
	}
	return ""
}

func heroPostflopPositionalEdge(position string) float64 {
	switch strings.ToLower(strings.TrimSpace(position)) {
	case "btn", "btn_sb":
		return 0.10
	case "co":
		return 0.07
	case "hj":
		return 0.04
	case "mp":
		return 0.01
	case "utg":
		return -0.02
	case "sb", "bb":
		return -0.05
	default:
		return 0
	}
}

func heroHasInitiative(input ai.DecisionInput) bool {
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	if stage == "" {
		stage = "preflop"
	}
	currentAggressor := lastAggressorForStage(input.RecentActionLog, stage)
	if currentAggressor == input.AIUserID {
		return true
	}
	if currentAggressor != "" {
		return false
	}
	previous := previousStageName(stage)
	if previous != "" {
		previousAggressor := lastAggressorForStage(input.RecentActionLog, previous)
		if previousAggressor == input.AIUserID {
			return true
		}
		if previousAggressor != "" {
			return false
		}
	}
	return lastAggressorForStage(input.RecentActionLog, "preflop") == input.AIUserID
}

func boardRangeAdvantage(input ai.DecisionInput, board []domain.Card) float64 {
	if len(board) == 0 {
		return 0
	}
	wetness := boardWetness(input.CommunityCards)
	ranks := topBoardRanks(board)
	highBoard := ranks[0]
	broadwayCount := 0
	paired := len(ranks) < len(board)
	for _, rank := range ranks {
		if rank >= 11 {
			broadwayCount++
		}
	}
	heroPFA := lastAggressorForStage(input.RecentActionLog, "preflop") == input.AIUserID
	score := 0.0
	if heroPFA {
		if highBoard >= 13 || broadwayCount >= 2 {
			score += 0.11
		}
		if wetness <= 0.40 {
			score += 0.09
		}
		if paired {
			score += 0.04
		}
		if highBoard <= 10 && wetness >= 0.58 {
			score -= 0.12
		}
	} else {
		if highBoard <= 10 && wetness >= 0.58 {
			score += 0.09
		}
		if highBoard >= 13 && wetness <= 0.42 {
			score -= 0.08
		}
	}
	score += heroPostflopPositionalEdge(input.PreflopPosition) * 0.35
	if activeOpponentCount(input) >= 3 {
		score *= 0.75
	}
	return clampFloat(score, -0.20, 0.22)
}

func latestBoardScareScore(board []domain.Card) float64 {
	if len(board) < 4 {
		return 0
	}
	last := board[len(board)-1]
	prev := board[:len(board)-1]
	score := 0.0
	if last.Rank >= 11 {
		score += 0.08
		if last.Rank == 14 {
			score += 0.03
		}
	}
	prevSuitCount := map[domain.Suit]int{}
	currentSuitCount := map[domain.Suit]int{}
	for _, card := range prev {
		prevSuitCount[card.Suit]++
	}
	for _, card := range board {
		currentSuitCount[card.Suit]++
	}
	for suit, count := range currentSuitCount {
		prevCount := prevSuitCount[suit]
		if prevCount <= 2 && count == 3 {
			score += 0.08
		}
		if prevCount <= 3 && count == 4 {
			score += 0.10
		}
	}
	for _, card := range prev {
		gap := last.Rank - card.Rank
		if gap < 0 {
			gap = -gap
		}
		if gap <= 1 {
			score += 0.02
			break
		}
	}
	for _, card := range prev {
		if card.Rank == last.Rank && last.Rank >= 10 {
			score += 0.04
			break
		}
	}
	return clampFloat(score, 0, 0.26)
}

func topBoardRanks(board []domain.Card) []int {
	seen := map[int]bool{}
	ranks := make([]int, 0, len(board))
	for _, card := range board {
		if seen[card.Rank] {
			continue
		}
		seen[card.Rank] = true
		ranks = append(ranks, card.Rank)
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] > ranks[j] })
	return ranks
}

func pairStrengthScore(hole []domain.Card, board []domain.Card) float64 {
	if len(hole) < 2 || len(board) == 0 {
		return 0.10
	}
	boardRanks := topBoardRanks(board)
	if len(boardRanks) == 0 {
		return 0.10
	}
	highestBoard := boardRanks[0]
	secondBoard := 0
	if len(boardRanks) > 1 {
		secondBoard = boardRanks[1]
	}
	if hole[0].Rank == hole[1].Rank {
		rank := hole[0].Rank
		switch {
		case rank > highestBoard:
			return 0.72
		case rank >= secondBoard && secondBoard > 0:
			return 0.48
		default:
			return 0.36
		}
	}
	bestMatch := 0
	for _, h := range hole {
		for _, b := range board {
			if h.Rank == b.Rank && h.Rank > bestMatch {
				bestMatch = h.Rank
			}
		}
	}
	if bestMatch > 0 {
		switch {
		case bestMatch >= highestBoard:
			return 0.62
		case secondBoard > 0 && bestMatch >= secondBoard:
			return 0.47
		default:
			return 0.34
		}
	}
	overcards := 0
	for _, h := range hole {
		if h.Rank > highestBoard {
			overcards++
		}
	}
	switch {
	case overcards >= 2:
		return 0.20
	case overcards == 1:
		return 0.16
	case hole[0].Rank == 14 || hole[1].Rank == 14:
		return 0.14
	default:
		return 0.10
	}
}

func boardMadeAndDrawStrength(hole []domain.Card, board []domain.Card) (float64, float64) {
	madeScore := pairStrengthScore(hole, board)
	if len(hole)+len(board) >= 5 {
		cards := append(append([]domain.Card{}, board...), hole...)
		best, _, _ := domain.BestOfSeven(cards)
		switch best.Category {
		case 8:
			madeScore = 0.99
		case 7:
			madeScore = 0.97
		case 6:
			madeScore = 0.95
		case 5:
			madeScore = 0.92
		case 4:
			madeScore = 0.86
		case 3:
			madeScore = 0.76
		case 2:
			madeScore = 0.68
		case 1:
			madeScore = clampFloat(maxFloat(madeScore, 0.32), 0.32, 0.72)
		default:
			madeScore = clampFloat(madeScore, 0.08, 0.24)
		}
	}
	drawScore := 0.0
	flushDraw := hasFlushDraw(hole, board)
	openEnded := hasOpenEndedStraightDraw(hole, board)
	if flushDraw {
		drawScore += 0.38
	}
	if openEnded {
		drawScore += 0.34
	} else if hasGutshotStraightDraw(hole, board) {
		drawScore += 0.18
	}
	if flushDraw && openEnded {
		drawScore += 0.12
	}
	if len(board) == 3 {
		ranks := topBoardRanks(board)
		if len(ranks) > 0 {
			for _, card := range hole {
				if card.Rank > ranks[0] {
					drawScore += 0.05
				}
			}
		}
	}
	return clampFloat(madeScore, 0.05, 0.99), clampFloat(drawScore, 0, 0.84)
}

func parseDecisionCards(input ai.DecisionInput) ([]domain.Card, []domain.Card, bool) {
	hole := make([]domain.Card, 0, len(input.HoleCards))
	board := make([]domain.Card, 0, len(input.CommunityCards))
	used := map[domain.Card]bool{}
	for _, raw := range input.HoleCards {
		card, ok := parseCardText(raw)
		if !ok || used[card] {
			return nil, nil, false
		}
		used[card] = true
		hole = append(hole, card)
	}
	for _, raw := range input.CommunityCards {
		card, ok := parseCardText(raw)
		if !ok || used[card] {
			return nil, nil, false
		}
		used[card] = true
		board = append(board, card)
	}
	return hole, board, len(hole) >= 2
}

func flushBlockerScore(hole []domain.Card, board []domain.Card) float64 {
	if len(hole) < 2 || len(board) < 3 {
		return 0
	}
	suitCount := map[domain.Suit]int{}
	for _, card := range board {
		suitCount[card.Suit]++
	}
	maxSuitCount := 0
	var targetSuit domain.Suit
	for suit, count := range suitCount {
		if count > maxSuitCount {
			maxSuitCount = count
			targetSuit = suit
		}
	}
	if maxSuitCount < 3 {
		return 0
	}
	heroSuitCount := 0
	score := 0.0
	for _, card := range hole {
		if card.Suit != targetSuit {
			continue
		}
		heroSuitCount++
		switch {
		case card.Rank == 14:
			score = maxFloat(score, 0.40)
		case card.Rank == 13:
			score = maxFloat(score, 0.31)
		case card.Rank == 12:
			score = maxFloat(score, 0.24)
		case card.Rank >= 10:
			score = maxFloat(score, 0.17)
		default:
			score = maxFloat(score, 0.10)
		}
	}
	if heroSuitCount == 0 {
		return 0
	}
	if maxSuitCount+heroSuitCount >= 5 {
		return 0
	}
	if maxSuitCount >= 4 {
		score *= 0.55
	}
	return clampFloat(score, 0, 0.42)
}

func broadwayBlockerScore(hole []domain.Card, board []domain.Card) float64 {
	if len(hole) < 2 || len(board) < 5 {
		return 0
	}
	highRanks := 0
	for _, card := range board {
		if card.Rank >= 11 {
			highRanks++
		}
	}
	if highRanks < 2 {
		return 0
	}
	score := 0.0
	for _, card := range hole {
		switch card.Rank {
		case 14:
			score = maxFloat(score, 0.12)
		case 13:
			score = maxFloat(score, 0.08)
		case 12:
			score = maxFloat(score, 0.05)
		}
	}
	return score
}

func riverHeroBlockerScore(hole []domain.Card, board []domain.Card, handCategoryRank int) float64 {
	score := flushBlockerScore(hole, board)
	if handCategoryRank <= 1 {
		score += broadwayBlockerScore(hole, board)
	}
	return clampFloat(score, 0, 0.58)
}

func riverMissedDrawScore(hole []domain.Card, board []domain.Card, handCategoryRank int) float64 {
	if len(hole) < 2 || len(board) < 5 || handCategoryRank >= 2 {
		return 0
	}
	turnBoard := board[:4]
	score := 0.0
	turnFlushDraw := hasFlushDraw(hole, turnBoard)
	turnOpenEnded := hasOpenEndedStraightDraw(hole, turnBoard)
	turnGutshot := !turnOpenEnded && hasGutshotStraightDraw(hole, turnBoard)
	if turnFlushDraw && handCategoryRank < 5 {
		score += 0.24
	}
	if turnOpenEnded && handCategoryRank < 4 {
		score += 0.20
	} else if turnGutshot && handCategoryRank < 4 {
		score += 0.10
	}
	if turnFlushDraw && turnOpenEnded && handCategoryRank < 4 {
		score += 0.08
	}
	if handCategoryRank == 1 {
		score *= 0.52
	} else {
		score += 0.03
	}
	return clampFloat(score, 0, 0.52)
}

func riverShowdownValueScore(hole []domain.Card, board []domain.Card, handCategoryRank int, pairScore float64) float64 {
	switch {
	case handCategoryRank >= 3:
		return 0.96
	case handCategoryRank == 2:
		return 0.78
	case handCategoryRank == 1:
		score := 0.28 + pairScore*0.52
		if pairScore >= 0.60 {
			score += 0.08
		}
		return clampFloat(score, 0.28, 0.72)
	default:
		if len(hole) < 2 || len(board) == 0 {
			return 0.08
		}
		ranks := topBoardRanks(board)
		if len(ranks) == 0 {
			return 0.08
		}
		highestBoard := ranks[0]
		overcards := 0
		aceHigh := false
		for _, card := range hole {
			if card.Rank > highestBoard {
				overcards++
			}
			if card.Rank == 14 {
				aceHigh = true
			}
		}
		score := 0.06 + float64(overcards)*0.04
		if aceHigh {
			score += 0.03
		}
		return clampFloat(score, 0.05, 0.22)
	}
}

func activeOpponentCallStationScore(input ai.DecisionInput) float64 {
	total := 0.0
	active := 0
	for _, player := range input.Players {
		if player.UserID == input.AIUserID || player.Folded || player.AllIn {
			continue
		}
		active++
		score := 0.0
		profile := input.Profiles[player.UserID]
		profileText := strings.ToLower(strings.TrimSpace(profile.Style + " " + profile.Advice + " " + strings.Join(profile.Tendencies, " ")))
		if containsAny(profileText,
			"calling-station", "station", "calling",
			"跟注站", "爱跟", "爱跟到底", "跟得多", "粘",
		) {
			score += 0.10
		}
		_, looseBias, aggressionBias, _ := profileRangeBias(profile)
		if looseBias > 0.12 {
			score += 0.04
		}
		if aggressionBias < 0.10 {
			score += 0.02
		}
		stats := input.OpponentStats[player.UserID]
		if stats.Hands >= 3 {
			if stats.FoldRate <= 0.20 {
				score += 0.08
			}
			if stats.VPIP >= 0.38 && stats.PFR <= 0.16 {
				score += 0.07
			}
			if stats.ShowdownRate >= 0.32 {
				score += 0.04
			}
		}
		total += score
	}
	if active == 0 {
		return 0
	}
	return clampFloat(total/float64(active), 0, 0.28)
}

func opponentHandWeight(input ai.DecisionInput, villain ai.PlayerSnapshot, hole []domain.Card, summary visibleActionSummary, board []domain.Card) float64 {
	score, isPair, isSuited, highRank, gap := preflopHoleStrengthCards(hole)
	madeScore, drawScore := boardMadeAndDrawStrength(hole, board)
	stats := input.OpponentStats[villain.UserID]
	profile := input.Profiles[villain.UserID]
	tightBias, looseBias, aggressionBias, trapBias := profileRangeBias(profile)
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	aggressiveLine := summary.CurrentStageAgg > 0 || strings.EqualFold(villain.LastAction, "bet") || strings.EqualFold(villain.LastAction, "allin")
	callingLine := summary.CurrentStageCalls > 0 || strings.EqualFold(villain.LastAction, "call")
	weight := 1.0
	if tightBias > 0 {
		weight *= 1.0 + tightBias*(score-0.46)*1.8
	}
	if looseBias > 0 {
		weight *= 1.0 + looseBias*(0.57-score)*1.5
		if isSuited && gap <= 3 && !isPair {
			weight += 0.05 * looseBias
		}
	}

	if stats.Hands >= 3 {
		tightness := clampFloat((0.28-stats.VPIP)*1.4, -0.20, 0.20)
		weight *= 1.0 + tightness*(score-0.48)*1.7
		if stats.PFR >= 0.26 {
			weight *= 1.0 + 0.10*(score-0.40)
			if isPair || highRank >= 12 {
				weight += 0.04
			}
		}
		if stats.VPIP >= 0.42 {
			weight *= 1.0 + 0.18*(0.55-score)
			if isSuited && gap <= 3 && !isPair {
				weight += 0.05
			}
		}
	}

	switch stage {
	case "preflop":
		if summary.PreflopRaises > 0 || strings.EqualFold(villain.LastAction, "bet") || strings.EqualFold(villain.LastAction, "allin") {
			weight *= 0.28 + 1.75*score
			weight *= 1.0 + aggressionBias*0.14*score
			if isPair {
				weight += 0.18
			}
			if highRank >= 13 {
				weight += 0.10
			}
			if !isPair && !isSuited && highRank <= 11 && gap >= 4 {
				weight *= 0.62
			}
		} else if summary.PreflopCalls > 0 || callingLine {
			weight *= 0.52 + 1.05*score
			weight *= 1.0 + looseBias*0.18 + trapBias*0.08*score
			if isSuited {
				weight += 0.08
			}
			if gap <= 2 {
				weight += 0.05
			}
		} else if strings.EqualFold(villain.LastAction, "check") {
			weight *= 0.74 + 0.52*score
			if isSuited {
				weight += 0.04
			}
		}
	default:
		wetness := boardWetness(input.CommunityCards)
		if aggressiveLine {
			weight *= 0.18 + 1.50*madeScore + 0.55*drawScore + 0.18*score
			weight *= 1.0 + aggressionBias*0.16*(madeScore+drawScore*0.6)
			if wetness > 0.55 {
				weight *= 1.0 + 0.12*drawScore
			} else {
				weight *= 1.0 + 0.08*madeScore
			}
			if villain.AllIn {
				weight *= 1.05 + 0.12*madeScore + 0.08*drawScore
			}
			if villain.RoundContrib >= input.RoundBet && input.RoundBet > 0 {
				weight *= 1.02 + 0.12*madeScore
			}
		} else if callingLine {
			weight *= 0.34 + 1.05*madeScore + 0.72*drawScore + 0.16*score
			weight *= 1.0 + looseBias*0.16 + trapBias*0.08*madeScore
			if isPair && highRank >= 10 {
				weight += 0.05
			}
		} else if strings.EqualFold(villain.LastAction, "check") {
			weight *= 0.70 + 0.60*madeScore + 0.40*drawScore + 0.12*(1.0-score)
		} else {
			weight *= 0.52 + 0.78*madeScore + 0.38*drawScore + 0.18*score
		}
		if stats.Hands >= 3 {
			if stats.PFR >= 0.26 && stats.AggressionFactor >= 2.2 && aggressiveLine {
				weight *= 1.04 + 0.10*madeScore + 0.06*drawScore
			}
			if stats.FoldRate <= 0.20 && callingLine {
				weight *= 1.02 + 0.08*madeScore + 0.06*drawScore
			}
			if stats.ShowdownRate >= 0.34 && stats.ShowdownWinRate <= 0.47 && callingLine {
				weight *= 1.03 + 0.05*(1.0-madeScore)
			}
		}
	}

	if isPair {
		weight += 0.04
	}
	if isSuited && stage != "preflop" && drawScore > 0 {
		weight += 0.04
	}
	if gap <= 1 && highRank >= 10 {
		weight += 0.03
	}
	return clampFloat(weight, 0.07, 4.5)
}

func monteCarloTrialCount(stage string, opponents int) int {
	stage = strings.ToLower(strings.TrimSpace(stage))
	trials := 220
	switch stage {
	case "preflop":
		trials = 300
	case "flop":
		trials = 260
	case "turn":
		trials = 220
	case "river":
		trials = 170
	}
	if opponents <= 1 {
		trials += 40
	} else if opponents >= 4 {
		trials -= 40
	}
	return clampInt(trials, 120, 360)
}

func estimateMonteCarloEquity(input ai.DecisionInput) (float64, bool) {
	if len(input.HoleCards) < 2 {
		return 0, false
	}
	hero := make([]domain.Card, 0, 2)
	board := make([]domain.Card, 0, 5)
	used := map[domain.Card]bool{}

	for _, raw := range input.HoleCards {
		card, ok := parseCardText(raw)
		if !ok || used[card] {
			return 0, false
		}
		used[card] = true
		hero = append(hero, card)
	}
	for _, raw := range input.CommunityCards {
		card, ok := parseCardText(raw)
		if !ok || used[card] {
			return 0, false
		}
		used[card] = true
		board = append(board, card)
	}
	if len(hero) != 2 || len(board) > 5 {
		return 0, false
	}

	villains := make([]ai.PlayerSnapshot, 0, len(input.Players))
	for _, p := range input.Players {
		if p.UserID == input.AIUserID || p.Folded {
			continue
		}
		villains = append(villains, p)
	}
	opponents := len(villains)
	if opponents <= 0 {
		return 1, true
	}

	deck := make([]domain.Card, 0, 52-len(used))
	for s := domain.Clubs; s <= domain.Spades; s++ {
		for r := 2; r <= 14; r++ {
			c := domain.Card{Rank: r, Suit: s}
			if !used[c] {
				deck = append(deck, c)
			}
		}
	}
	needBoard := 5 - len(board)
	if needBoard < 0 {
		needBoard = 0
	}
	needTotal := needBoard + opponents*2
	if needTotal <= 0 {
		needTotal = 1
	}
	if len(deck) < needTotal {
		return 0, false
	}

	trials := monteCarloTrialCount(input.Stage, opponents)
	unweightedScore := 0.0
	weightedScore := 0.0
	totalWeight := 0.0
	work := make([]domain.Card, len(deck))
	seed := int64(decisionHash64(input, "mc-equity"))
	rng := mathrand.New(mathrand.NewSource(seed))
	actionSummary := summarizeVisibleActionsByUser(input.RecentActionLog, input.Stage)

	for t := 0; t < trials; t++ {
		copy(work, deck)
		for i := 0; i < needTotal; i++ {
			j := i + rng.Intn(len(work)-i)
			work[i], work[j] = work[j], work[i]
		}
		drawn := work[:needTotal]

		offset := 0
		boardNow := append([]domain.Card{}, board...)
		if needBoard > 0 {
			boardNow = append(boardNow, drawn[offset:offset+needBoard]...)
			offset += needBoard
		}

		heroCards := append(append([]domain.Card{}, boardNow...), hero...)
		heroValue, _, _ := domain.BestOfSeven(heroCards)

		heroBest := true
		tiedOpponents := 0
		sampleWeight := 1.0
		for i := 0; i < opponents; i++ {
			oppHole := []domain.Card{drawn[offset], drawn[offset+1]}
			offset += 2
			likelihood := opponentHandWeight(input, villains[i], oppHole, actionSummary[villains[i].UserID], board)
			sampleWeight *= 0.45 + 0.55*likelihood
			oppCards := append(append([]domain.Card{}, boardNow...), oppHole...)
			oppValue, _, _ := domain.BestOfSeven(oppCards)
			cmp := domain.CompareHandValue(oppValue, heroValue)
			if cmp > 0 {
				heroBest = false
				break
			}
			if cmp == 0 {
				tiedOpponents++
			}
		}
		totalWeight += sampleWeight
		if heroBest {
			share := 1.0 / float64(tiedOpponents+1)
			unweightedScore += share
			weightedScore += share * sampleWeight
		}
	}
	if totalWeight > 0 {
		return clampFloat(weightedScore/totalWeight, 0.01, 0.99), true
	}
	return clampFloat(unweightedScore/float64(trials), 0.01, 0.99), true
}

func boardWetness(community []string) float64 {
	if len(community) == 0 {
		return 0.25
	}
	suitCount := map[byte]int{}
	rankCount := map[int]int{}
	ranks := make([]int, 0, len(community))
	for _, raw := range community {
		rank, suit, ok := parseCardTextRankSuit(raw)
		if !ok {
			continue
		}
		suitCount[suit]++
		rankCount[rank]++
		ranks = append(ranks, rank)
	}
	if len(ranks) == 0 {
		return 0.25
	}
	wet := 0.22
	maxSuit := 0
	for _, count := range suitCount {
		if count > maxSuit {
			maxSuit = count
		}
	}
	if maxSuit >= 3 {
		wet += 0.22
	}
	if maxSuit >= 4 {
		wet += 0.20
	}
	pairCount := 0
	for _, count := range rankCount {
		if count >= 2 {
			pairCount++
		}
	}
	if pairCount > 0 {
		wet -= 0.05
	}
	uniq := make([]int, 0, len(rankCount))
	for rank := range rankCount {
		uniq = append(uniq, rank)
		if rank == 14 {
			uniq = append(uniq, 1)
		}
	}
	sort.Ints(uniq)
	connect := 0
	for i := 1; i < len(uniq); i++ {
		gap := uniq[i] - uniq[i-1]
		if gap > 0 && gap <= 2 {
			connect++
		}
	}
	if connect >= 2 {
		wet += 0.18
	}
	if connect >= 3 {
		wet += 0.10
	}
	highCount := 0
	for _, rank := range ranks {
		if rank >= 10 {
			highCount++
		}
	}
	if highCount >= 3 {
		wet += 0.08
	}
	return clampFloat(wet, 0.05, 0.95)
}

func deterministicRoll(input ai.DecisionInput, salt string) float64 {
	return float64(decisionHash64(input, salt)%10000) / 10000.0
}

func decisionHash64(input ai.DecisionInput, salt string) uint64 {
	h := fnv.New64a()
	parts := []string{
		input.RoomID,
		fmt.Sprintf("%d", input.HandID),
		fmt.Sprintf("%d", input.StateVersion),
		input.AIUserID,
		input.Stage,
		input.PreflopTier,
		input.MadeHandStrength,
		strings.Join(input.HoleCards, ","),
		strings.Join(input.CommunityCards, ","),
		strings.Join(input.DrawFlags, ","),
		fmt.Sprintf("%d|%d|%d|%d|%d", input.Pot, input.RoundBet, input.CallAmount, input.MinBet, input.MinRaise),
		salt,
	}
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	start := 0
	if len(input.RecentActionLog) > 8 {
		start = len(input.RecentActionLog) - 8
	}
	for i := start; i < len(input.RecentActionLog); i++ {
		l := input.RecentActionLog[i]
		_, _ = h.Write([]byte(l.UserID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(l.Action))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(fmt.Sprintf("%d", l.Amount)))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

type fallbackBetSizingContext struct {
	Initiative      bool
	RangeAdv        float64
	ScareScore      float64
	BlockerScore    float64
	MissedDrawScore float64
	CappedScore     float64
	BarrelCount     int
}

func chooseFallbackBetAmount(input ai.DecisionInput, betMin int, mode string, roll float64, wetness float64, pressure float64, ctx fallbackBetSizingContext, params StrategyParams) int {
	if betMin <= 0 {
		betMin = 1
	}
	if input.Stack <= betMin {
		return input.Stack
	}
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	if stage == "preflop" && input.RoundBet > 0 {
		blindBase := maxInt(input.OpenBetMin, input.BetMin)
		if blindBase <= 0 {
			blindBase = betMin
		}
		mult := 2.3 + 1.4*roll
		switch mode {
		case "value":
			mult = 2.7 + 1.8*roll
		case "polarize":
			mult = 3.2 + 2.2*roll
		case "bluff":
			mult = 2.0 + 1.2*roll
		case "semi_bluff":
			mult = 2.4 + 1.5*roll
		case "probe":
			mult = 2.1 + 1.3*roll
		}
		target := input.CallAmount + int(float64(blindBase)*mult)
		return clampInt(target, betMin, input.Stack)
	}

	fraction := 0.40
	switch mode {
	case "value":
		fraction = 0.50 + 0.35*roll + 0.18*wetness - 0.06*pressure
	case "polarize":
		fraction = 0.72 + 0.36*roll + 0.12*wetness
	case "semi_bluff":
		fraction = 0.45 + 0.30*roll + 0.13*wetness
	case "bluff":
		fraction = 0.28 + 0.22*roll + 0.08*wetness - 0.08*pressure
	case "probe":
		fraction = 0.24 + 0.18*roll - 0.06*pressure
	}
	if input.CallAmount > 0 {
		fraction += 0.12
	}
	if stage == "river" && (mode == "value" || mode == "polarize") {
		fraction += 0.07
	}
	if stage == "flop" && ctx.Initiative && ctx.RangeAdv >= 0.08 && wetness <= 0.42 {
		switch mode {
		case "probe", "bluff":
			fraction -= params.FlopDryRangeBetReduction
		case "value":
			fraction -= 0.02
		}
	}
	if stage == "turn" && ctx.BarrelCount >= 1 && (ctx.ScareScore >= 0.10 || ctx.CappedScore >= 0.08) {
		switch mode {
		case "polarize", "bluff", "semi_bluff":
			fraction += 0.08 + ctx.ScareScore*params.TurnScareSizingWeight + ctx.CappedScore*0.18
		case "value":
			fraction += 0.05 + ctx.CappedScore*0.12
		default:
			fraction += 0.03 + ctx.ScareScore*0.12
		}
	}
	if stage == "river" {
		barrelBonus := float64(ctx.BarrelCount) * 0.03
		if barrelBonus > 0.10 {
			barrelBonus = 0.10
		}
		switch mode {
		case "polarize", "bluff":
			fraction += 0.08 + ctx.BlockerScore*0.24 + ctx.MissedDrawScore*params.RiverMissedDrawSizingWeight + ctx.ScareScore*0.18 + barrelBonus
		case "value":
			fraction += 0.04 + ctx.CappedScore*0.12
		case "probe":
			if ctx.BlockerScore < 0.12 && ctx.RangeAdv < 0.08 {
				fraction -= 0.04
			}
		}
	}
	if !ctx.Initiative && stage != "river" && mode == "probe" {
		fraction -= 0.02
	}
	if ctx.CappedScore >= 0.10 && (mode == "value" || mode == "semi_bluff") {
		fraction += 0.02
	}
	fraction = clampFloat(fraction, 0.18, 1.35)
	target := input.CallAmount + int(float64(maxInt(1, input.Pot))*fraction)
	return clampInt(target, betMin, input.Stack)
}

func shouldFallbackJam(input ai.DecisionInput, equity float64, pressure float64, opponents int, roll float64) bool {
	if input.Stack <= 0 {
		return false
	}
	pot := maxInt(1, input.Pot)
	spr := float64(input.Stack) / float64(pot)
	minCommit := maxInt(input.MinRaise, input.MinBet)
	if minCommit <= 0 {
		minCommit = 1
	}
	switch {
	case equity >= 0.90 && spr <= 1.60:
		return true
	case equity >= 0.82 && spr <= 1.10 && opponents <= 2:
		return true
	case equity >= 0.76 && pressure >= 0.95 && opponents <= 2 && roll < 0.60:
		return true
	case equity >= 0.70 && input.Stack <= minCommit:
		return true
	default:
		return false
	}
}

func callPotOdds(input ai.DecisionInput) float64 {
	if input.CallAmount <= 0 {
		return 0
	}
	return float64(input.CallAmount) / float64(maxInt(1, input.Pot+input.CallAmount))
}

func drawEquityBonus(input ai.DecisionInput) float64 {
	strongDraw, weakDraw := hasDrawPotential(input.DrawFlags)
	stageWeight := drawStageWeight(input.Stage)
	bonus := 0.0
	if strongDraw {
		bonus += 0.08 * stageWeight
	} else if weakDraw {
		bonus += 0.04 * stageWeight
	}
	return bonus
}

func decisionAllowedByInput(input ai.DecisionInput, decision ai.Decision) bool {
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	if action == "" {
		return false
	}
	allowed := map[string]bool{}
	for _, a := range input.AllowedActions {
		allowed[strings.ToLower(strings.TrimSpace(a))] = true
	}
	if !allowed[action] {
		return false
	}
	switch action {
	case "check", "call", "allin", "fold":
		return true
	case "bet":
		min := input.MinBet
		if input.RoundBet > 0 {
			min = input.MinRaise
		}
		if min <= 0 {
			min = 1
		}
		return decision.Amount >= min && decision.Amount <= input.Stack
	default:
		return false
	}
}

func decisionPassesEVGuard(input ai.DecisionInput, decision ai.Decision, equity float64) bool {
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	if !decisionAllowedByInput(input, decision) {
		return false
	}
	canFold := false
	for _, a := range input.AllowedActions {
		if strings.EqualFold(strings.TrimSpace(a), "fold") {
			canFold = true
			break
		}
	}
	facingBet := input.CallAmount > 0
	potOdds := callPotOdds(input)
	effectiveEquity := clampFloat(equity+drawEquityBonus(input), 0, 1)
	opponents := activeOpponentCount(input)

	switch action {
	case "fold":
		if facingBet && canFold && effectiveEquity > potOdds+0.08 {
			return false
		}
		return true
	case "call":
		if !facingBet {
			return true
		}
		if !canFold {
			return true
		}
		if effectiveEquity+0.02 < potOdds {
			return false
		}
		return true
	case "allin":
		pressure := estimateFallbackPressure(input)
		jamRoll := deterministicRoll(input, "jam-guard")
		if shouldFallbackJam(input, equity, pressure, opponents, jamRoll) {
			return true
		}
		if facingBet && canFold {
			required := float64(input.Stack) / float64(maxInt(1, input.Pot+input.Stack))
			if effectiveEquity+0.04 < required {
				return false
			}
		}
		if !facingBet && equity < 0.40 && drawEquityBonus(input) < 0.02 {
			return false
		}
		return true
	case "bet":
		if facingBet && canFold {
			if effectiveEquity+0.03 < potOdds && drawEquityBonus(input) < 0.05 {
				return false
			}
			return true
		}
		if !facingBet && opponents >= 3 {
			pot := maxInt(1, input.Pot)
			if equity < 0.30 && drawEquityBonus(input) < 0.03 && decision.Amount > int(0.85*float64(pot)) {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func guardAIDecision(input ai.DecisionInput, decision ai.Decision, fallback ai.Decision) ai.Decision {
	equity := estimateFallbackEquity(input)
	if decisionPassesEVGuard(input, decision, equity) && !shouldPreferFallbackDecision(input, decision, fallback, equity) {
		return decision
	}
	if decisionPassesEVGuard(input, fallback, equity) {
		return fallback
	}

	// Keep a conservative legal fallback to avoid retries on invalid model outputs.
	if input.CallAmount == 0 {
		if decisionAllowedByInput(input, ai.Decision{Action: "check", Amount: 0}) {
			return ai.Decision{Action: "check", Amount: 0}
		}
	}
	if decisionAllowedByInput(input, ai.Decision{Action: "call", Amount: 0}) {
		return ai.Decision{Action: "call", Amount: 0}
	}
	if decisionAllowedByInput(input, ai.Decision{Action: "fold", Amount: 0}) {
		return ai.Decision{Action: "fold", Amount: 0}
	}
	if decisionAllowedByInput(input, ai.Decision{Action: "allin", Amount: 0}) {
		return ai.Decision{Action: "allin", Amount: 0}
	}
	return fallback
}

func fallbackPreflopDecision(
	input ai.DecisionInput,
	can func(string) bool,
	canBet bool,
	betWithMode func(string) ai.Decision,
	primaryRoll float64,
	altRoll float64,
	pressure float64,
	foldEqAdj float64,
	valueAdj float64,
	potOdds float64,
	equity float64,
) (ai.Decision, bool) {
	position := strings.ToLower(strings.TrimSpace(input.PreflopPosition))
	if position == "" {
		position = "unknown"
	}
	effectiveBB := input.EffectiveStackBB
	if effectiveBB <= 0 {
		effectiveBB = float64(input.Stack) / float64(maxInt(1, input.OpenBetMin))
	}
	handScore, isPair, isSuited, highRank, gap := preflopHandScore(input)
	tightness := preflopPositionTightness(position)
	handScore = clampFloat(handScore*0.62+equity*0.38+valueAdj*0.22-pressure*0.02, 0.01, 0.99)
	facingRaise := input.PreflopFacingRaise || (input.CallAmount > 0 && input.RoundBet > input.OpenBetMin)
	raiseLevel := preflopRaiseLevel(input)

	if effectiveBB <= 12.5 && can("allin") {
		jamThreshold := 0.64 + tightness*0.24 + (raiseLevel-2.0)*0.03
		if isPair {
			jamThreshold -= 0.06
		}
		if highRank >= 14 {
			jamThreshold -= 0.03
		}
		if position == "bb" {
			jamThreshold -= 0.02
		}
		jamThreshold = clampFloat(jamThreshold, 0.44, 0.84)
		if handScore >= jamThreshold || (handScore >= jamThreshold-0.03 && altRoll < 0.32) {
			return ai.Decision{Action: "allin", Amount: 0}, true
		}
	}

	if facingRaise {
		if canBet {
			threeBetThreshold := 0.74 + tightness*0.22 + (raiseLevel-2.0)*0.04 - valueAdj*0.18
			if isPair && highRank >= 11 {
				threeBetThreshold -= 0.08
			}
			if position == "bb" || position == "btn" || position == "btn_sb" {
				threeBetThreshold -= 0.03
			}
			threeBetThreshold = clampFloat(threeBetThreshold, 0.56, 0.90)
			threeBetChance := clampFloat(0.07+valueAdj+foldEqAdj*0.28-pressure*0.12, 0.01, 0.34)
			if handScore >= threeBetThreshold || (handScore >= threeBetThreshold-0.04 && primaryRoll < threeBetChance) {
				mode := "value"
				if handScore < threeBetThreshold+0.02 && primaryRoll < threeBetChance*0.66 {
					mode = "bluff"
				}
				return betWithMode(mode), true
			}
		}

		callThreshold := 0.53 + tightness*0.24 + (raiseLevel-2.0)*0.05 - valueAdj*0.16
		if position == "bb" {
			callThreshold -= 0.07
		}
		if position == "btn" || position == "btn_sb" || position == "co" {
			callThreshold -= 0.03
		}
		if isSuited {
			callThreshold -= 0.02
		}
		if isPair {
			callThreshold -= 0.03
		}
		if gap <= 2 && highRank >= 10 {
			callThreshold -= 0.01
		}
		if effectiveBB <= 20 {
			callThreshold += 0.03
		}
		if handScore+0.02 < potOdds {
			callThreshold += 0.10
		}
		callThreshold = clampFloat(callThreshold, 0.30, 0.88)
		if can("call") && (handScore >= callThreshold || (handScore >= callThreshold-0.03 && primaryRoll < 0.22)) {
			return ai.Decision{Action: "call", Amount: 0}, true
		}
		if can("fold") {
			return ai.Decision{Action: "fold", Amount: 0}, true
		}
		if can("call") {
			return ai.Decision{Action: "call", Amount: 0}, true
		}
		if can("check") {
			return ai.Decision{Action: "check", Amount: 0}, true
		}
		return ai.Decision{Action: "fold", Amount: 0}, true
	}

	openThreshold := 0.50 + tightness*0.30 - valueAdj*0.18
	if isPair {
		openThreshold -= 0.04
	}
	if isSuited {
		openThreshold -= 0.015
	}
	if gap <= 1 && highRank >= 10 {
		openThreshold -= 0.015
	}
	if effectiveBB <= 18 {
		openThreshold += 0.02
	}
	openThreshold = clampFloat(openThreshold, 0.28, 0.78)

	opponents := activeOpponentCount(input)
	stealChance := clampFloat(0.10+foldEqAdj+valueAdj*0.12-float64(opponents-1)*0.03-tightness*0.10, 0.02, 0.55)
	if canBet && (handScore >= openThreshold || altRoll < stealChance) {
		mode := "probe"
		if handScore >= openThreshold+0.11 || isPair {
			mode = "value"
		} else if altRoll < stealChance*0.55 {
			mode = "bluff"
		}
		return betWithMode(mode), true
	}
	if can("check") {
		return ai.Decision{Action: "check", Amount: 0}, true
	}
	if can("call") && handScore >= openThreshold-0.08 {
		return ai.Decision{Action: "call", Amount: 0}, true
	}
	if can("fold") {
		return ai.Decision{Action: "fold", Amount: 0}, true
	}
	if can("call") {
		return ai.Decision{Action: "call", Amount: 0}, true
	}
	return ai.Decision{Action: "check", Amount: 0}, true
}

func fallbackDecision(input ai.DecisionInput) ai.Decision {
	return fallbackDecisionWithParams(input, currentStrategyParams())
}

func fallbackDecisionWithParams(input ai.DecisionInput, params StrategyParams) ai.Decision {
	allowed := map[string]bool{}
	for _, a := range input.AllowedActions {
		allowed[strings.ToLower(a)] = true
	}
	can := func(action string) bool {
		return allowed[action]
	}
	betMin := input.MinBet
	if input.RoundBet > 0 {
		betMin = input.MinRaise
	}
	if betMin <= 0 {
		betMin = 1
	}
	clampBet := func(amount int) int {
		return clampInt(amount, betMin, input.Stack)
	}
	if len(allowed) == 0 {
		return ai.Decision{Action: "fold", Amount: 0}
	}
	canBet := can("bet") && input.Stack >= betMin
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	if stage == "" {
		stage = "preflop"
	}

	primaryRoll := deterministicRoll(input, "primary")
	altRoll := deterministicRoll(input, "alt")
	betRoll := deterministicRoll(input, "bet")
	equity := estimateFallbackEquity(input)
	pressure := estimateFallbackPressure(input)
	wetness := boardWetness(input.CommunityCards)
	aggression := tableAggressionScore(input.RecentActionLog, stage)
	profileFoldAdj, profileValueAdj, profileTrapAdj := profileStrategyAdjustments(input.Profiles)
	statsFoldAdj, statsValueAdj, statsTrapAdj := opponentStatsStrategyAdjustments(input.OpponentStats)
	foldEqAdj := clampFloat(profileFoldAdj+statsFoldAdj, -0.22, 0.22)
	valueAdj := clampFloat(profileValueAdj+statsValueAdj, -0.10, 0.26)
	trapAdj := clampFloat(profileTrapAdj+statsTrapAdj, 0, 0.30)
	strongDraw, weakDraw := hasDrawPotential(input.DrawFlags)
	opponents := activeOpponentCount(input)
	facingBet := input.CallAmount > 0
	potOdds := callPotOdds(input)
	equityWithDraw := clampFloat(equity+drawEquityBonus(input), 0, 1)
	spr := float64(input.Stack) / float64(maxInt(1, input.Pot))
	shortStack := spr <= 1.35 || input.Stack <= maxInt(input.OpenBetMin*7, input.CallAmount+input.BetMin*2)
	heroHole, heroBoard, cardsOK := parseDecisionCards(input)
	heroCategoryRank := input.HandCategoryRank
	pairScore := 0.0
	blockerScore := 0.0
	missedDrawScore := 0.0
	showdownValueScore := 0.0
	stationScore := activeOpponentCallStationScore(input)
	initiative := heroHasInitiative(input)
	rangeAdv := 0.0
	scareScore := 0.0
	stageActions := stageActionCount(input.RecentActionLog, stage)
	lastStageActor := lastActorForStage(input.RecentActionLog, stage)
	barrelCount := heroPostflopBarrelCount(input)
	previousBarrelCalled := previousStreetHeroBarrelCalled(input)
	lineCapScore := visibleRangeCapScore(input)
	if cardsOK {
		if len(heroHole)+len(heroBoard) >= 5 {
			best, _, _ := domain.BestOfSeven(append(append([]domain.Card{}, heroBoard...), heroHole...))
			heroCategoryRank = best.Category
		}
		pairScore = pairStrengthScore(heroHole, heroBoard)
		rangeAdv = boardRangeAdvantage(input, heroBoard)
		scareScore = latestBoardScareScore(heroBoard)
		if stage == "river" {
			blockerScore = riverHeroBlockerScore(heroHole, heroBoard, heroCategoryRank)
			missedDrawScore = riverMissedDrawScore(heroHole, heroBoard, heroCategoryRank)
			showdownValueScore = riverShowdownValueScore(heroHole, heroBoard, heroCategoryRank, pairScore)
		}
	}

	betWithMode := func(mode string) ai.Decision {
		amount := chooseFallbackBetAmount(input, betMin, mode, betRoll, wetness, pressure, fallbackBetSizingContext{
			Initiative:      initiative,
			RangeAdv:        rangeAdv,
			ScareScore:      scareScore,
			BlockerScore:    blockerScore,
			MissedDrawScore: missedDrawScore,
			CappedScore:     lineCapScore,
			BarrelCount:     barrelCount,
		}, params)
		return ai.Decision{Action: "bet", Amount: clampBet(amount)}
	}

	if stage == "preflop" && input.OpenBetMin > 0 && len(input.HoleCards) >= 2 {
		if d, ok := fallbackPreflopDecision(input, can, canBet, betWithMode, primaryRoll, altRoll, pressure, foldEqAdj, valueAdj, potOdds, equity); ok {
			return d
		}
	}

	if stage == "river" && cardsOK {
		if facingBet && can("call") && heroCategoryRank <= 1 {
			callEdge := pairScore*0.18 + blockerScore*0.20 + aggression*0.08 + trapAdj*0.08 - float64(opponents-1)*0.06
			if input.CallAmount > input.Pot {
				callEdge -= 0.06
			} else if input.CallAmount <= maxInt(1, input.Pot/4) {
				callEdge += 0.03
			}
			passivePenalty := clampFloat(0.12-aggression*0.10-trapAdj*0.08, 0, 0.10)
			required := clampFloat(potOdds+0.05-callEdge+passivePenalty, 0.10, 0.88)
			if equityWithDraw >= required || (pairScore >= 0.60 && blockerScore >= 0.20 && equityWithDraw+0.02 >= required) {
				return ai.Decision{Action: "call", Amount: 0}
			}
			if can("fold") {
				return ai.Decision{Action: "fold", Amount: 0}
			}
		}

		if !facingBet && can("check") && heroCategoryRank == 1 && pairScore < params.RiverCheckbackPairMax && stationScore >= params.RiverCheckbackStationThreshold && blockerScore < 0.18 && missedDrawScore < 0.14 {
			return ai.Decision{Action: "check", Amount: 0}
		}

		if !facingBet && canBet {
			thinValueChance := 0.10 + valueAdj*0.95 + pairScore*0.34 - aggression*0.07 - float64(opponents-1)*0.05 + lineCapScore*0.25 - stationScore*params.RiverThinValueStationPenalty
			if heroCategoryRank >= 2 {
				thinValueChance += 0.14
			}
			if previousBarrelCalled {
				thinValueChance += 0.04
			}
			thinValueChance = clampFloat(thinValueChance, 0.05, 0.78)
			if (heroCategoryRank >= 2 || pairScore >= 0.58) && primaryRoll < thinValueChance {
				mode := "probe"
				if heroCategoryRank >= 2 || pairScore >= 0.66 {
					mode = "value"
				}
				return betWithMode(mode)
			}

			bluffChance := 0.03 + foldEqAdj + blockerScore*0.34 + missedDrawScore*params.RiverMissedDrawBluffWeight - showdownValueScore*params.RiverShowdownPenaltyWeight - stationScore*params.RiverStationPenaltyWeight - equity*0.16 - aggression*0.08 - float64(opponents-1)*0.06 + lineCapScore*0.45 + float64(barrelCount)*0.03
			if blockerScore >= 0.28 {
				bluffChance += 0.06
			}
			if previousBarrelCalled {
				bluffChance += scareScore * 0.22
			}
			if barrelCount >= 2 && (missedDrawScore >= 0.18 || blockerScore >= 0.18) {
				bluffChance += params.RiverTripleBarrelBonus
			}
			if pairScore >= 0.52 {
				bluffChance -= 0.08
			}
			if showdownValueScore >= 0.30 && missedDrawScore < 0.18 {
				bluffChance -= 0.06
			}
			bluffChance = clampFloat(bluffChance, 0.01, 0.50)
			if heroCategoryRank <= 1 && pairScore < 0.54 && (blockerScore >= 0.18 || missedDrawScore >= 0.18) && altRoll < bluffChance {
				mode := "bluff"
				if (blockerScore >= 0.34 && foldEqAdj > 0.08 && opponents <= 2) || (barrelCount >= 2 && lineCapScore >= 0.08 && scareScore >= 0.08) {
					mode = "polarize"
				}
				return betWithMode(mode)
			}
		}
	}

	if equity >= 0.80 {
		if can("allin") && shouldFallbackJam(input, equity, pressure, opponents, altRoll) {
			return ai.Decision{Action: "allin", Amount: 0}
		}
		trapChance := clampFloat(0.07+trapAdj+aggression*0.14+(1.0-wetness)*0.08, 0.03, 0.48)
		if canBet && (!can("check") || facingBet || primaryRoll > trapChance) {
			mode := "value"
			if equity >= 0.90 && (stage == "river" || opponents <= 2) && altRoll > 0.58 {
				mode = "polarize"
			}
			return betWithMode(mode)
		}
		if facingBet && can("call") {
			return ai.Decision{Action: "call", Amount: 0}
		}
		if can("check") {
			return ai.Decision{Action: "check", Amount: 0}
		}
	}

	if equity >= 0.60 || (strongDraw && equity >= 0.50) {
		if facingBet {
			raiseChance := clampFloat(0.08+valueAdj+aggression*0.06-pressure*0.12, 0.02, 0.42)
			if strongDraw {
				raiseChance = clampFloat(raiseChance+0.08, 0.02, 0.50)
			}
			if canBet && primaryRoll < raiseChance {
				mode := "semi_bluff"
				if equity >= 0.72 {
					mode = "value"
				}
				return betWithMode(mode)
			}
			callChance := 0.74 - pressure*0.45 + valueAdj*0.35
			if strongDraw {
				callChance += 0.18
			} else if weakDraw {
				callChance += 0.08
			}
			if shortStack {
				callChance += 0.06
			}
			if equityWithDraw+0.03 < potOdds {
				callChance -= 0.40
			}
			callChance = clampFloat(callChance, 0.08, 0.93)
			if can("call") && (primaryRoll < callChance || !can("fold")) {
				return ai.Decision{Action: "call", Amount: 0}
			}
			if can("fold") {
				return ai.Decision{Action: "fold", Amount: 0}
			}
			if can("check") {
				return ai.Decision{Action: "check", Amount: 0}
			}
		} else {
			stabChance := 0.22 + valueAdj + foldEqAdj*0.40 + aggression*0.05 + lineCapScore*0.85 + float64(barrelCount)*0.04
			if strongDraw {
				stabChance += 0.11
			}
			if initiative {
				stabChance += 0.10 + rangeAdv*0.75 + heroPostflopPositionalEdge(input.PreflopPosition)*0.30
				if stage == "flop" && wetness <= 0.42 && opponents <= 2 {
					stabChance += 0.08
				}
				if stage == "turn" {
					stabChance += 0.06 + scareScore*0.55
				}
			} else {
				stabChance += rangeAdv*0.35 - 0.04
				if lastStageActor != "" && lastStageActor != input.AIUserID && stageActions > 0 {
					stabChance += 0.05
				}
			}
			if previousBarrelCalled {
				if stage == "turn" {
					stabChance += 0.08 + scareScore*0.30
				}
				if stage == "river" {
					stabChance += 0.05 + blockerScore*0.16
				}
			}
			if stage == "flop" {
				stabChance += 0.05
			}
			if stage == "turn" && !initiative && wetness >= 0.58 && scareScore < 0.05 {
				stabChance -= 0.07
			}
			stabChance = clampFloat(stabChance, 0.08, 0.86)
			if canBet && primaryRoll < stabChance {
				mode := "probe"
				if equity >= 0.72 || (initiative && rangeAdv >= 0.08 && stage == "flop") {
					mode = "value"
				} else if initiative && stage == "turn" && (scareScore >= 0.12 || (previousBarrelCalled && lineCapScore >= 0.10)) {
					mode = "polarize"
				} else if initiative && stage == "river" && heroCategoryRank <= 1 && barrelCount >= 2 && blockerScore >= 0.18 && lineCapScore >= 0.08 {
					mode = "polarize"
				} else if strongDraw {
					mode = "semi_bluff"
				}
				return betWithMode(mode)
			}
			if can("check") {
				return ai.Decision{Action: "check", Amount: 0}
			}
			if can("call") {
				return ai.Decision{Action: "call", Amount: 0}
			}
		}
	}

	if facingBet {
		defendChance := 0.16 - pressure*0.52 + aggression*0.12 + foldEqAdj*0.15
		if weakDraw {
			defendChance += 0.11
		}
		if strongDraw {
			defendChance += 0.20
		}
		if stage == "river" && strings.EqualFold(strings.TrimSpace(input.HandCategory), "high_card") {
			defendChance -= 0.08
		}
		if equityWithDraw+0.02 < potOdds {
			defendChance -= 0.32
		}
		defendChance = clampFloat(defendChance, 0.02, 0.46)
		if can("call") && (primaryRoll < defendChance || (input.CallAmount <= input.Pot/7 && altRoll < 0.50)) {
			return ai.Decision{Action: "call", Amount: 0}
		}

		bluffRaiseChance := 0.0
		if canBet {
			bluffRaiseChance = 0.04 + foldEqAdj + aggression*0.04 - pressure*0.12
			if strongDraw {
				bluffRaiseChance += 0.08
			}
			bluffRaiseChance = clampFloat(bluffRaiseChance, 0, 0.22)
		}
		if canBet && altRoll < bluffRaiseChance {
			mode := "bluff"
			if strongDraw {
				mode = "semi_bluff"
			}
			return betWithMode(mode)
		}
		if can("fold") {
			return ai.Decision{Action: "fold", Amount: 0}
		}
		if can("call") {
			if can("fold") && equityWithDraw+0.02 < potOdds {
				return ai.Decision{Action: "fold", Amount: 0}
			}
			return ai.Decision{Action: "call", Amount: 0}
		}
	} else {
		stealChance := 0.11 + foldEqAdj + (0.12 - equity*0.08) - aggression*0.08 - float64(opponents-1)*0.03 + lineCapScore*0.70 + float64(barrelCount)*0.03
		if stage == "river" {
			stealChance += missedDrawScore*params.RiverStealMissedDrawWeight - showdownValueScore*params.RiverStealShowdownPenalty - stationScore*params.RiverStealStationPenalty
		}
		if strongDraw {
			stealChance += 0.08
		}
		if initiative {
			stealChance += 0.10 + rangeAdv*0.85 + heroPostflopPositionalEdge(input.PreflopPosition)*0.25
			if stage == "flop" && wetness <= 0.42 {
				stealChance += 0.08
			}
			if stage == "turn" {
				stealChance += scareScore * 0.40
			}
		} else {
			stealChance += rangeAdv*0.20 - 0.02
		}
		if previousBarrelCalled {
			if stage == "turn" {
				stealChance += 0.08 + scareScore*0.45
			}
			if stage == "river" {
				stealChance += blockerScore*0.36 + scareScore*0.24
				if barrelCount >= 2 {
					stealChance += params.RiverTripleBarrelBonus
				}
			}
		}
		if stage == "turn" || stage == "river" {
			stealChance += 0.04
		}
		if stage == "river" && pairScore >= 0.52 {
			stealChance -= 0.08
		}
		if stage == "river" && (blockerScore < 0.14 || opponents >= 3) {
			stealChance -= 0.10
		}
		if stage == "river" && missedDrawScore < 0.14 && blockerScore < 0.14 {
			stealChance -= 0.10
		}
		if stage == "preflop" && (input.PreflopTier == "playable" || input.PreflopTier == "speculative") {
			stealChance += 0.03
		}
		stealChance = clampFloat(stealChance, 0.05, 0.82)
		if canBet && altRoll < stealChance {
			mode := "bluff"
			if strongDraw {
				mode = "semi_bluff"
			}
			if stage == "turn" && initiative && previousBarrelCalled && (scareScore >= 0.10 || lineCapScore >= 0.10) && !strongDraw {
				mode = "polarize"
			}
			if stage == "river" && initiative && barrelCount >= 2 && (blockerScore >= 0.18 || missedDrawScore >= 0.18) && pairScore < 0.48 && opponents <= 2 {
				mode = "polarize"
			}
			return betWithMode(mode)
		}
		if can("check") {
			return ai.Decision{Action: "check", Amount: 0}
		}
	}

	if can("check") {
		return ai.Decision{Action: "check", Amount: 0}
	}
	if can("call") {
		return ai.Decision{Action: "call", Amount: 0}
	}
	if can("fold") {
		return ai.Decision{Action: "fold", Amount: 0}
	}
	if can("allin") {
		return ai.Decision{Action: "allin", Amount: 0}
	}
	if canBet {
		return ai.Decision{Action: "bet", Amount: clampBet(betMin)}
	}
	return ai.Decision{Action: "fold", Amount: 0}
}

func copyActionLogs(logs []domain.ActionLog) []ai.ActionLog {
	out := make([]ai.ActionLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, ai.ActionLog{UserID: l.UserID, Username: l.Username, Action: l.Action, Amount: l.Amount, Stage: l.Stage})
	}
	return out
}

func copyPlayers(room *Room, game *domain.GameState) []ai.PlayerSnapshot {
	isAIByUserID := map[string]bool{}
	for _, p := range room.Players {
		isAIByUserID[p.UserID] = p.IsAI
	}
	players := make([]ai.PlayerSnapshot, 0, len(game.Players))
	for _, p := range game.Players {
		players = append(players, ai.PlayerSnapshot{
			UserID:       p.UserID,
			Username:     p.Username,
			IsAI:         isAIByUserID[p.UserID],
			SeatIndex:    p.SeatIndex,
			Stack:        p.Stack,
			Folded:       p.Folded,
			AllIn:        p.AllIn,
			Contributed:  p.Contributed,
			RoundContrib: p.RoundContrib,
			LastAction:   p.LastAction,
			Won:          p.Won,
		})
	}
	return players
}

func allowedActionsForPlayer(game *domain.GameState, p *domain.GamePlayer) ([]string, int, int, int) {
	allowed := []string{}
	if p.Folded || p.AllIn {
		return allowed, 0, 0, 0
	}
	diff := game.RoundBet - p.RoundContrib
	if diff == 0 {
		allowed = append(allowed, "check")
	}
	if diff > 0 {
		allowed = append(allowed, "call")
	}
	minBet := 0
	if game.RoundBet == 0 && p.Stack >= game.OpenBetMin {
		allowed = append(allowed, "bet")
		minBet = game.OpenBetMin
	}
	minRaise := 0
	if game.RoundBet > 0 {
		need := diff + game.BetMin
		if p.Stack >= need {
			allowed = append(allowed, "bet")
			minRaise = need
		}
	}
	if p.Stack > 0 {
		allowed = append(allowed, "allin")
	}
	allowed = append(allowed, "fold")
	return allowed, diff, minBet, minRaise
}

type handObservation struct {
	Seen            bool
	VPIP            bool
	PFR             bool
	PostflopAgg     int
	PostflopCall    int
	FoldActions     int
	DecisionActions int
	WentToShowdown  bool
	WonShowdown     bool
}

func collectHandObservations(game *domain.GameState) map[string]*handObservation {
	out := map[string]*handObservation{}
	if game == nil {
		return out
	}
	for _, gp := range game.Players {
		if gp == nil {
			continue
		}
		out[gp.UserID] = &handObservation{Seen: true}
	}
	for _, log := range game.ActionLogs {
		if log.UserID == "" {
			continue
		}
		obs := out[log.UserID]
		if obs == nil {
			obs = &handObservation{Seen: true}
			out[log.UserID] = obs
		}
		stage := strings.ToLower(strings.TrimSpace(log.Stage))
		action := strings.ToLower(strings.TrimSpace(log.Action))
		switch action {
		case "small_blind", "big_blind":
			continue
		case "check":
			obs.DecisionActions++
		case "call":
			obs.DecisionActions++
			if stage == "preflop" {
				obs.VPIP = true
			} else {
				obs.PostflopCall++
			}
		case "bet":
			obs.DecisionActions++
			if stage == "preflop" {
				obs.VPIP = true
				obs.PFR = true
			} else {
				obs.PostflopAgg++
			}
		case "allin":
			obs.DecisionActions++
			if stage == "preflop" {
				obs.VPIP = true
			} else {
				obs.PostflopAgg++
			}
		case "fold":
			obs.DecisionActions++
			obs.FoldActions++
		}
	}
	showdown := game.Result != nil && game.Result.Reason == "showdown"
	if showdown {
		for _, gp := range game.Players {
			if gp == nil || gp.Folded {
				continue
			}
			obs := out[gp.UserID]
			if obs == nil {
				obs = &handObservation{Seen: true}
				out[gp.UserID] = obs
			}
			obs.WentToShowdown = true
			if gp.Won > 0 {
				obs.WonShowdown = true
			}
		}
	}
	return out
}

func (m *MemoryStore) updateOpponentStatsFromFinishedHandLocked(room *Room, aiUserID string) {
	if room == nil || room.Game == nil || room.Game.Stage != domain.StageFinished {
		return
	}
	mem := m.ensureAIMemory(room, aiUserID)
	if mem.LastStatsHand == room.HandCounter {
		return
	}

	obsByUser := collectHandObservations(room.Game)
	for uid, obs := range obsByUser {
		if uid == "" || uid == aiUserID || obs == nil || !obs.Seen {
			continue
		}
		if mem.OpponentStats == nil {
			mem.OpponentStats = map[string]*OpponentStat{}
		}
		stat := mem.OpponentStats[uid]
		if stat == nil {
			stat = &OpponentStat{}
			mem.OpponentStats[uid] = stat
		}
		stat.Hands++
		if obs.VPIP {
			stat.VPIPHands++
		}
		if obs.PFR {
			stat.PFRHands++
		}
		stat.PostflopAggActions += obs.PostflopAgg
		stat.PostflopCallActions += obs.PostflopCall
		stat.FoldActions += obs.FoldActions
		stat.DecisionActions += obs.DecisionActions
		if obs.WentToShowdown {
			stat.WentToShowdownHands++
		}
		if obs.WonShowdown {
			stat.WonShowdownHands++
		}
	}
	mem.LastStatsHand = room.HandCounter
}

func (m *MemoryStore) enqueueAIDecisionLocked(room *Room) {
	m.enqueueAIDecisionLockedWithRetry(room, 2)
}

func (m *MemoryStore) enqueueAIDecisionLockedWithRetry(room *Room, retriesLeft int) {
	if room == nil || room.Game == nil || room.Status != RoomPlaying {
		return
	}
	if m.aiWorkers[room.RoomID] {
		return
	}
	if len(room.Game.Players) == 0 || room.Game.TurnPos < 0 || room.Game.TurnPos >= len(room.Game.Players) {
		return
	}
	turn := room.Game.Players[room.Game.TurnPos]
	if !turn.IsAI && !turn.AIManaged {
		return
	}
	memory := m.ensureAIMemory(room, turn.UserID)
	memory.LastDecisionHand = room.HandCounter
	input, ok := buildAIDecisionInput(room, turn, memory)
	if !ok {
		return
	}
	fallback := fallbackDecision(input)
	baseline := fallback
	input.BaselineDecision = &baseline
	input.DecisionOptions = buildDecisionOptions(input, currentStrategyParams(), baseline)
	task := &aiDecisionTask{
		RoomID:          room.RoomID,
		HandID:          room.HandCounter,
		ExpectedVersion: room.StateVersion,
		AIUserID:        turn.UserID,
		ActionID:        fmt.Sprintf("ai-%s-%d", turn.UserID, room.StateVersion),
		Input:           input,
		Fallback:        fallback,
		RetriesLeft:     retriesLeft,
	}
	m.aiWorkers[room.RoomID] = true
	m.aiQueue <- aiTaskEnvelope{kind: aiJobDecide, decide: task}
}

func (m *MemoryStore) enqueueAISummaryLocked(room *Room) {
	if room == nil || room.Game == nil || room.Game.Stage != domain.StageFinished {
		return
	}
	canSummarize := m.summaryLLMEnabled()
	community := make([]string, 0, len(room.Game.CommunityCards))
	for _, c := range room.Game.CommunityCards {
		community = append(community, cardToText(c))
	}
	winners := []string{}
	reason := ""
	if room.Game.Result != nil {
		winners = append(winners, room.Game.Result.Winners...)
		reason = room.Game.Result.Reason
	}
	for _, gp := range room.Game.Players {
		if !shouldSummarizeAIMemoryForPlayer(room, gp, room.HandCounter) {
			continue
		}
		m.updateOpponentStatsFromFinishedHandLocked(room, gp.UserID)
		mem := m.ensureAIMemory(room, gp.UserID)
		if mem.LastSummarizedHand == room.HandCounter {
			continue
		}
		if !canSummarize {
			continue
		}
		input := ai.SummaryInput{
			RoomID:                room.RoomID,
			HandID:                room.HandCounter,
			AIUserID:              gp.UserID,
			AIUsername:            gp.Username,
			ActionLogs:            copyActionLogs(room.Game.ActionLogs),
			Winners:               winners,
			Reason:                reason,
			CommunityCards:        community,
			Players:               copyPlayers(room, room.Game),
			ExistingMemory:        append([]string{}, mem.HandSummaries...),
			ExistingProfile:       cloneProfiles(mem.OpponentProfiles),
			ExistingOpponentStats: cloneOpponentStats(mem.OpponentStats),
		}
		m.aiQueue <- aiTaskEnvelope{kind: aiJobSummarize, summary: &aiSummaryTask{RoomID: room.RoomID, HandID: room.HandCounter, Input: input}}
	}
}

func (m *MemoryStore) aiEventLoop() {
	for task := range m.aiQueue {
		switch task.kind {
		case aiJobDecide:
			if task.decide == nil {
				continue
			}
			decision := task.decide.Fallback
			service := m.currentAIService()
			if service != nil && service.Enabled() {
				llmDecision, err := service.DecideAction(context.Background(), task.decide.Input)
				if err == nil {
					llmDecision = materializeDecisionOption(task.decide.Input, llmDecision)
					if decisionAllowedByInput(task.decide.Input, llmDecision) {
						decision = llmDecision
					}
				}
			}
			decision = guardAIDecision(task.decide.Input, decision, task.decide.Fallback)
			m.applyActionFromAI(task.decide, decision)
		case aiJobSummarize:
			if task.summary == nil {
				continue
			}
			m.mu.RLock()
			busy := m.aiWorkers[task.summary.RoomID]
			m.mu.RUnlock()
			if busy {
				m.aiQueue <- task
				time.Sleep(20 * time.Millisecond)
				continue
			}
			service := m.currentAIService()
			if service == nil || !service.Enabled() {
				continue
			}
			summary, err := service.SummarizeHand(context.Background(), task.summary.Input)
			if err != nil {
				continue
			}
			m.applySummary(task.summary, summary)
		}
	}
}

func (m *MemoryStore) applySummary(task *aiSummaryTask, summary ai.Summary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[task.RoomID]
	if !ok || r.AIMemory == nil {
		return
	}
	mem := m.ensureAIMemory(r, task.Input.AIUserID)
	if mem.LastSummarizedHand == task.HandID {
		return
	}
	mem.LastSummarizedHand = task.HandID
	if summary.HandSummary != "" {
		mem.HandSummaries = append(mem.HandSummaries, summary.HandSummary)
		if len(mem.HandSummaries) > MaxAISummaries {
			mem.HandSummaries = mem.HandSummaries[len(mem.HandSummaries)-MaxAISummaries:]
		}
	}
	for uid, profile := range summary.OpponentProfiles {
		if uid == "" {
			continue
		}
		if uid == task.Input.AIUserID {
			continue
		}
		if mem.OpponentProfiles == nil {
			mem.OpponentProfiles = map[string]*OpponentProfile{}
		}
		tend := make([]string, len(profile.Tendencies))
		copy(tend, profile.Tendencies)
		mem.OpponentProfiles[uid] = &OpponentProfile{Style: profile.Style, Tendencies: tend, Advice: profile.Advice}
	}
}

func (m *MemoryStore) BenchmarkStatus() BenchmarkStatus {
	status := BenchmarkStatus{CurrentParams: currentStrategyParams(), PersistedParams: currentStrategyParams(), ConfigPath: strategyConfigPath(""), AISettings: AIRuntimeStatus{ConfigPath: aiRuntimeConfigPath("")}}
	if m != nil && m.benchmark != nil {
		status = m.benchmark.Status()
	}
	status.AISettings = m.currentAISettings()
	return status
}

func (m *MemoryStore) StartBenchmark() (BenchmarkStatus, error) {
	if m == nil || m.benchmark == nil {
		return BenchmarkStatus{}, fmt.Errorf("benchmark unavailable")
	}
	status, err := m.benchmark.Start()
	status.AISettings = m.currentAISettings()
	return status, err
}

func (m *MemoryStore) StopBenchmark() BenchmarkStatus {
	status := BenchmarkStatus{CurrentParams: currentStrategyParams(), PersistedParams: currentStrategyParams(), ConfigPath: strategyConfigPath(""), AISettings: AIRuntimeStatus{ConfigPath: aiRuntimeConfigPath("")}}
	if m != nil && m.benchmark != nil {
		status = m.benchmark.Stop()
	}
	status.AISettings = m.currentAISettings()
	return status
}

func (m *MemoryStore) AIRuntimeStatus() AIRuntimeStatus {
	return m.currentAISettings()
}
