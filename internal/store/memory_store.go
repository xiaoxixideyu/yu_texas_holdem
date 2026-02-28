package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	"wait_flowers":   {},
	"solve_universe": {},
	"tea_refill":     {},
	"countdown":      {},
	"thinker_mode":   {},
	"dawn_table":     {},
	"cappuccino":     {},
	"showtime":       {},
	"you_act_i_act":  {},
	"something_here": {},
	"mind_game":      {},
	"script_seen":    {},
	"allin_warning":  {},
	"just_this":      {},
	"easy_sigh":      {},
	"fold_now":       {},
	"you_call_i_show": {},
	"take_the_shot":  {},
	"pressure_on":    {},
	"tilt_alert":     {},
	"nh":             {},
	"gg":             {},
	"luck_is_skill":  {},
	"next_real":      {},
}


type RoomPlayer struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Seat     int    `json:"seat"`
	Stack    int    `json:"stack"`
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
}

type quickChatSeenKey struct {
	ActionID    string
	CreatedAtMs int64
}

type MemoryStore struct {
	mu           sync.RWMutex
	users        map[string]*Session
	rooms        map[string]*Room
	lastActive   map[string]int64 // userID -> unix timestamp
	nextRoom     int64
	roomsVersion int64
}

func NewMemoryStore() *MemoryStore {
	ms := &MemoryStore{
		users:      map[string]*Session{},
		rooms:      map[string]*Room{},
		lastActive: map[string]int64{},
	}
	go ms.idleCleanupLoop()
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

func (m *MemoryStore) ListRooms() ([]Room, int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		copyRoom := *r
		copyRoom.Game = nil
		list = append(list, copyRoom)
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
		Players:              []RoomPlayer{{UserID: owner.UserID, Username: owner.Username, Seat: 0, Stack: 10000}},
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
	r.Players = append(r.Players, RoomPlayer{UserID: s.UserID, Username: s.Username, Seat: len(r.Players), Stack: 10000})
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) GetRoom(roomID string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rooms[roomID]
	return r, ok
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
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if r.OwnerUserID != userID {
		return nil, errors.New("only owner can start")
	}
	if r.Status != RoomWaiting {
		return nil, errors.New("game already started")
	}
	if len(r.Players) < 2 {
		return nil, errors.New("at least 2 players needed")
	}
	g, err := m.buildGameFromRoom(r, nil)
	if err != nil {
		return nil, err
	}
	r.Game = g
	r.Status = RoomPlaying
	r.ActionSeen = map[string]bool{}
	if len(r.Players) > 0 {
		r.NextDealerPos = (g.DealerPos + 1) % len(r.Players)
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) LeaveRoom(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
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
		return nil, errors.New("user not in room")
	}

	if r.Status == RoomPlaying && r.Game != nil {
		r.Game.ForceLeaveForStore(userID)
	}

	r.Players = append(r.Players[:idx], r.Players[idx+1:]...)
	for i := range r.Players {
		r.Players[i].Seat = i
	}
	if len(r.Players) == 0 {
		delete(m.rooms, roomID)
		m.roomsVersion++
		return nil, nil
	}
	if r.OwnerUserID == userID {
		r.OwnerUserID = r.Players[0].UserID
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
		r.Game.FinishByLastStandingForStore()
		r.Status = RoomWaiting
	}

	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) NextHand(roomID, userID string) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if r.OwnerUserID != userID {
		return nil, errors.New("only owner can start next hand")
	}
	if r.Game == nil || r.Game.Stage != domain.StageFinished {
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
		return nil, err
	}
	r.Game = g
	r.Status = RoomPlaying
	r.ActionSeen = map[string]bool{}
	if len(r.Players) > 0 {
		r.NextDealerPos = (g.DealerPos + 1) % len(r.Players)
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
}

func (m *MemoryStore) ApplyAction(roomID, userID, actionID, action string, amount int, expectedVersion int64) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rooms[roomID]
	if !ok {
		return nil, errors.New("room not found")
	}
	if r.Game == nil || r.Status != RoomPlaying {
		return nil, errors.New("game not started")
	}
	if expectedVersion != r.StateVersion {
		return nil, errors.New("version conflict")
	}
	if actionID != "" && r.ActionSeen[actionID] {
		return r, nil
	}
	if err := r.Game.ApplyAction(userID, action, amount); err != nil {
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
	if r.Game.Stage == domain.StageFinished {
		r.Status = RoomWaiting
	}
	r.StateVersion++
	r.UpdatedAtUnix = time.Now().Unix()
	m.roomsVersion++
	return r, nil
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

// LeaveAllRooms removes the user from every room they are in.
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

// RemoveUser deletes the session and cleans up all rooms.
func (m *MemoryStore) RemoveUser(userID string) {
	m.LeaveAllRooms(userID)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, userID)
	delete(m.lastActive, userID)
}

const idleTimeout = 60 * 60 // 60 minutes in seconds

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
