package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	MaxAISummaries = 20
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

type RoomAIMemory struct {
	HandSummaries      []string                    `json:"handSummaries"`
	OpponentProfiles   map[string]*OpponentProfile `json:"opponentProfiles"`
	LastSummarizedHand int64                       `json:"lastSummarizedHand"`
}

type RoomPlayer struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Seat     int    `json:"seat"`
	Stack    int    `json:"stack"`
	IsAI     bool   `json:"isAi"`
}

type Room struct {
	RoomID               string       `json:"roomId"`
	Name                 string       `json:"name"`
	OpenBetMin           int          `json:"openBetMin"`
	BetMin               int          `json:"betMin"`
	OwnerUserID          string       `json:"ownerUserId"`
	Status               RoomStatus   `json:"status"`
	Players              []RoomPlayer `json:"players"`
	StateVersion         int64        `json:"stateVersion"`
	UpdatedAtUnix        int64        `json:"updatedAtUnix"`
	NextDealerPos        int          `json:"-"`
	Game                 *domain.GameState
	ActionSeen           map[string]bool
	QuickChats           []QuickChatEvent
	QuickChatSeen        map[string]bool
	QuickChatSeenOrder   []quickChatSeenKey
	QuickChatLastSentAt  map[string]int64
	QuickChatNextEventID int64
	AIMemory             map[string]*RoomAIMemory `json:"aiMemory"`
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
	AI ai.Service
}

type MemoryStore struct {
	mu           sync.RWMutex
	users        map[string]*Session
	rooms        map[string]*Room
	lastActive   map[string]int64
	nextRoom     int64
	nextAIUser   int64
	roomsVersion int64

	aiService ai.Service
	aiWorkers map[string]bool
	aiQueue   chan aiTaskEnvelope
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
	ms := &MemoryStore{
		users:      map[string]*Session{},
		rooms:      map[string]*Room{},
		lastActive: map[string]int64{},
		aiService:  aiSvc,
		aiWorkers:  map[string]bool{},
		aiQueue:    make(chan aiTaskEnvelope, 256),
	}
	go ms.idleCleanupLoop()
	go ms.aiEventLoop()
	return ms
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
	if _, err := rand.Read(b); err != nil {
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
		Players:              []RoomPlayer{{UserID: owner.UserID, Username: owner.Username, Seat: 0, Stack: 10000, IsAI: false}},
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
	for _, p := range r.Players {
		if p.UserID == s.UserID {
			return r, nil
		}
	}
	if r.Status != RoomWaiting {
		return nil, errors.New("room already playing")
	}
	r.Players = append(r.Players, RoomPlayer{UserID: s.UserID, Username: s.Username, Seat: len(r.Players), Stack: 10000, IsAI: false})
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
		UserID:   m.newAIUserID(),
		Username: aiName,
		Seat:     len(r.Players),
		Stack:    10000,
		IsAI:     true,
	}
	r.Players = append(r.Players, aiPlayer)
	r.AIMemory[aiPlayer.UserID] = &RoomAIMemory{HandSummaries: []string{}, OpponentProfiles: map[string]*OpponentProfile{}}
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

func cloneRoomLocked(r *Room) *Room {
	if r == nil {
		return nil
	}
	copyRoom := *r
	if r.Players != nil {
		copyRoom.Players = append([]RoomPlayer(nil), r.Players...)
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
			copyRoom.AIMemory[uid] = m2
		}
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
	for _, p := range r.Players {
		stack := p.Stack
		if stacks != nil {
			if v, ok := stacks[p.UserID]; ok {
				stack = v
			}
		}
		gps = append(gps, &domain.GamePlayer{
			UserID:    p.UserID,
			Username:  p.Username,
			IsAI:      p.IsAI,
			SeatIndex: p.Seat,
			Stack:     stack,
		})
	}
	dealerPos := r.NextDealerPos
	if len(gps) > 0 {
		dealerPos = ((dealerPos % len(gps)) + len(gps)) % len(gps)
	}
	return domain.NewGame(gps, dealerPos, r.OpenBetMin, r.BetMin)
}

func (m *MemoryStore) StartGame(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
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
	r.HandCounter++
	if len(r.Players) > 0 {
		r.NextDealerPos = (g.DealerPos + 1) % len(r.Players)
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
	idx := -1
	for i, p := range r.Players {
		if p.UserID == userID {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.mu.Unlock()
		return nil, errors.New("user not in room")
	}

	finishedByLeave := false
	if r.Status == RoomPlaying && r.Game != nil {
		beforeStage := r.Game.Stage
		r.Game.ForceLeaveForStore(userID)
		finishedByLeave = beforeStage != domain.StageFinished && r.Game.Stage == domain.StageFinished
	}

	isLeavingAI := r.Players[idx].IsAI
	r.Players = append(r.Players[:idx], r.Players[idx+1:]...)
	for i := range r.Players {
		r.Players[i].Seat = i
	}
	if isLeavingAI {
		delete(r.AIMemory, userID)
	}

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

func (m *MemoryStore) NextHand(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
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
	r.HandCounter++
	if len(r.Players) > 0 {
		r.NextDealerPos = (g.DealerPos + 1) % len(r.Players)
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	m.enqueueAIDecisionLocked(r)
	m.mu.Unlock()
	return r, nil
}

func (m *MemoryStore) ApplyAction(roomID, userID, actionID, action string, amount int, expectedVersion int64) (*Room, error) {
	m.mu.Lock()
	r, ok := m.rooms[roomID]
	if !ok {
		m.mu.Unlock()
		return nil, errors.New("room not found")
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

	_, err := m.ApplyAction(task.RoomID, task.AIUserID, task.ActionID, decision.Action, decision.Amount, task.ExpectedVersion)
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
		for _, p := range r.Players {
			if p.UserID == userID {
				roomIDs = append(roomIDs, rid)
				break
			}
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

func (m *MemoryStore) ensureAIMemory(room *Room, aiUserID string) *RoomAIMemory {
	if room.AIMemory == nil {
		room.AIMemory = map[string]*RoomAIMemory{}
	}
	if room.AIMemory[aiUserID] == nil {
		room.AIMemory[aiUserID] = &RoomAIMemory{HandSummaries: []string{}, OpponentProfiles: map[string]*OpponentProfile{}}
	}
	if room.AIMemory[aiUserID].OpponentProfiles == nil {
		room.AIMemory[aiUserID].OpponentProfiles = map[string]*OpponentProfile{}
	}
	return room.AIMemory[aiUserID]
}

func fallbackDecision(input ai.DecisionInput) ai.Decision {
	allowed := map[string]bool{}
	for _, a := range input.AllowedActions {
		allowed[strings.ToLower(a)] = true
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
		if amount > input.Stack {
			amount = input.Stack
		}
		return ai.Decision{Action: "bet", Amount: amount}
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

func (m *MemoryStore) enqueueAIDecisionLocked(room *Room) {
	m.enqueueAIDecisionLockedWithRetry(room, 2)
}

func (m *MemoryStore) enqueueAIDecisionLockedWithRetry(room *Room, retriesLeft int) {
	if !m.aiService.Enabled() || room == nil || room.Game == nil || room.Status != RoomPlaying {
		return
	}
	if m.aiWorkers[room.RoomID] {
		return
	}
	if len(room.Game.Players) == 0 || room.Game.TurnPos < 0 || room.Game.TurnPos >= len(room.Game.Players) {
		return
	}
	turn := room.Game.Players[room.Game.TurnPos]
	if !turn.IsAI {
		return
	}
	allowed, callAmount, minBet, minRaise := allowedActionsForPlayer(room.Game, turn)
	if len(allowed) == 0 {
		return
	}
	memory := m.ensureAIMemory(room, turn.UserID)
	community := make([]string, 0, len(room.Game.CommunityCards))
	for _, c := range room.Game.CommunityCards {
		community = append(community, cardToText(c))
	}
	input := ai.DecisionInput{
		RoomID:          room.RoomID,
		HandID:          room.HandCounter,
		StateVersion:    room.StateVersion,
		AIUserID:        turn.UserID,
		AIUsername:      turn.Username,
		Stage:           string(room.Game.Stage),
		Pot:             room.Game.Pot,
		RoundBet:        room.Game.RoundBet,
		OpenBetMin:      room.Game.OpenBetMin,
		BetMin:          room.Game.BetMin,
		CallAmount:      callAmount,
		MinBet:          minBet,
		MinRaise:        minRaise,
		Stack:           turn.Stack,
		AllowedActions:  allowed,
		CommunityCards:  community,
		Players:         copyPlayers(room, room.Game),
		RecentActionLog: copyActionLogs(room.Game.ActionLogs),
		MemorySummaries: append([]string{}, memory.HandSummaries...),
		Profiles:        cloneProfiles(memory.OpponentProfiles),
	}
	fallback := fallbackDecision(input)
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
	if !m.aiService.Enabled() || room == nil || room.Game == nil || room.Game.Stage != domain.StageFinished {
		return
	}
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
		if !gp.IsAI {
			continue
		}
		mem := m.ensureAIMemory(room, gp.UserID)
		if mem.LastSummarizedHand == room.HandCounter {
			continue
		}
		input := ai.SummaryInput{
			RoomID:          room.RoomID,
			HandID:          room.HandCounter,
			AIUserID:        gp.UserID,
			AIUsername:      gp.Username,
			ActionLogs:      copyActionLogs(room.Game.ActionLogs),
			Winners:         winners,
			Reason:          reason,
			CommunityCards:  community,
			Players:         copyPlayers(room, room.Game),
			ExistingMemory:  append([]string{}, mem.HandSummaries...),
			ExistingProfile: cloneProfiles(mem.OpponentProfiles),
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
			decision, err := m.aiService.DecideAction(context.Background(), task.decide.Input)
			if err != nil {
				decision = task.decide.Fallback
			}
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
			summary, err := m.aiService.SummarizeHand(context.Background(), task.summary.Input)
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
		if mem.OpponentProfiles == nil {
			mem.OpponentProfiles = map[string]*OpponentProfile{}
		}
		tend := make([]string, len(profile.Tendencies))
		copy(tend, profile.Tendencies)
		mem.OpponentProfiles[uid] = &OpponentProfile{Style: profile.Style, Tendencies: tend, Advice: profile.Advice}
	}
}
