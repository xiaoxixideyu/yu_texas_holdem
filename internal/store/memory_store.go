package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

type RoomPlayer struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Seat     int    `json:"seat"`
	Stack    int    `json:"stack"`
}

type Room struct {
	RoomID        string       `json:"roomId"`
	Name          string       `json:"name"`
	OpenBetMin    int          `json:"openBetMin"`
	BetMin        int          `json:"betMin"`
	OwnerUserID   string       `json:"ownerUserId"`
	Status        RoomStatus   `json:"status"`
	Players       []RoomPlayer `json:"players"`
	StateVersion  int64        `json:"stateVersion"`
	UpdatedAtUnix int64        `json:"updatedAtUnix"`
	NextDealerPos int          `json:"-"`
	Game          *domain.GameState
	ActionSeen    map[string]bool
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
		RoomID:        fmt.Sprintf("r-%d", rid),
		Name:          name,
		OpenBetMin:    openBetMin,
		BetMin:        betMin,
		OwnerUserID:   owner.UserID,
		Status:        RoomWaiting,
		Players:       []RoomPlayer{{UserID: owner.UserID, Username: owner.Username, Seat: 0, Stack: 10000}},
		StateVersion:  1,
		UpdatedAtUnix: time.Now().Unix(),
		NextDealerPos: 0,
		Game:          nil,
		ActionSeen:    map[string]bool{},
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
