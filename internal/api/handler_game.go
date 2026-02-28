package api

import (
	"net/http"
	"strconv"
	"strings"

	"texas_yu/internal/domain"
	"texas_yu/internal/store"
)

type GameHandler struct {
	Store *store.MemoryStore
}

type actionReq struct {
	ActionID        string `json:"actionId"`
	Type            string `json:"type"`
	Amount          int    `json:"amount"`
	RevealMask      int    `json:"revealMask,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type quickChatReq struct {
	ActionID string `json:"actionId"`
	PhraseID string `json:"phraseId"`
}

type gamePlayerView struct {
	UserID       string         `json:"userId"`
	Username     string         `json:"username"`
	SeatIndex    int            `json:"seatIndex"`
	Stack        int            `json:"stack"`
	Folded       bool           `json:"folded"`
	LastAction   string         `json:"lastAction"`
	Won          int            `json:"won"`
	Contributed  int            `json:"contributed"`
	BestHandName string         `json:"bestHandName,omitempty"`
	RevealMask   int            `json:"revealMask"`
	CanReveal    bool           `json:"canReveal"`
	HoleCards    []*domain.Card `json:"holeCards,omitempty"`
	IsTurn       bool           `json:"isTurn"`
	CanCheck     bool           `json:"canCheck"`
	CanCall      bool           `json:"canCall"`
	CanBet       bool           `json:"canBet"`
	CanRaise     bool           `json:"canRaise"`
	CanFold      bool           `json:"canFold"`
	CallAmount   int            `json:"callAmount"`
	MinBet       int            `json:"minBet"`
	MinRaise     int            `json:"minRaise"`
}

func (h *GameHandler) GetQuickChats(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, ok := h.Store.GetRoom(roomID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "room not found"})
		return
	}
	inRoom := false
	for _, p := range room.Players {
		if p.UserID == s.UserID {
			inRoom = true
			break
		}
	}
	if !inRoom {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "user not in room"})
		return
	}

	sinceEventID := int64(0)
	if sv := r.URL.Query().Get("sinceEventId"); sv != "" {
		if v, err := strconv.ParseInt(sv, 10, 64); err == nil && v > 0 {
			sinceEventID = v
		}
	}

	_, events, latestEventID, serverNowMs, err := h.Store.ListQuickChats(roomID, sinceEventID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	bubbleTTL, cooldown, retention := h.Store.QuickChatConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"events":        events,
		"latestEventId": latestEventID,
		"serverNowMs":   serverNowMs,
		"bubbleTtlMs":   bubbleTTL,
		"cooldownMs":    cooldown,
		"retentionMs":   retention,
		"phrases":       h.Store.QuickChatPhrases(),
	})
}

func (h *GameHandler) QuickChat(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	var req quickChatReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	_, event, retryAfterMs, err := h.Store.SendQuickChat(roomID, s.UserID, req.ActionID, req.PhraseID)
	if err != nil {
		status := http.StatusBadRequest
		body := map[string]any{"error": err.Error()}
		if err.Error() == "quick chat cooldown" {
			status = http.StatusTooManyRequests
			body["retryAfterMs"] = retryAfterMs
		}
		if err.Error() == "user not in room" {
			status = http.StatusForbidden
		}
		writeJSON(w, status, body)
		return
	}
	if event == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
		return
	}
	_, cooldown, _ := h.Store.QuickChatConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"chatEventId": event.EventID,
		"expireAtMs":  event.ExpireAtMs,
		"cooldownMs":  cooldown,
	})
}

func (h *GameHandler) GetState(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, ok := h.Store.GetRoom(roomID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "room not found"})
		return
	}
	since := int64(0)
	if sv := r.URL.Query().Get("sinceVersion"); sv != "" {
		if v, err := strconv.ParseInt(sv, 10, 64); err == nil {
			since = v
		}
	}
	if since > 0 && room.StateVersion == since {
		writeJSON(w, http.StatusOK, map[string]any{"notModified": true, "version": room.StateVersion})
		return
	}

	roomPlayers := make([]map[string]any, 0, len(room.Players))
	for _, p := range room.Players {
		roomPlayers = append(roomPlayers, map[string]any{
			"userId":   p.UserID,
			"username": p.Username,
			"seat":     p.Seat,
			"stack":    p.Stack,
		})
	}

	resp := map[string]any{
		"roomId":           room.RoomID,
		"roomName":         room.Name,
		"roomStatus":       room.Status,
		"ownerUserId":      room.OwnerUserID,
		"stateVersion":     room.StateVersion,
		"roomPlayers":      roomPlayers,
		"canStartNextHand": room.OwnerUserID == s.UserID && room.Game != nil && room.Game.Stage == domain.StageFinished,
	}
	if room.Game == nil {
		resp["game"] = nil
		writeJSON(w, http.StatusOK, resp)
		return
	}

	players := make([]gamePlayerView, 0, len(room.Game.Players))
	for idx, p := range room.Game.Players {
		isTurn := idx == room.Game.TurnPos && room.Game.Stage != domain.StageFinished
		canCheck := false
		canCall := false
		canBet := false
		canRaise := false
		canFold := false
		callAmount := 0
		minBet := 0
		minRaise := 0
		if isTurn && !p.Folded {
			diff := room.Game.RoundBet - p.RoundContrib
			canCheck = diff == 0
			canCall = diff > 0 && p.Stack >= diff
			if canCall {
				callAmount = diff
			}
			canBet = room.Game.RoundBet == 0 && p.Stack >= room.Game.OpenBetMin
			if canBet {
				minBet = room.Game.OpenBetMin
			}
			if room.Game.RoundBet > 0 {
				need := diff + room.Game.BetMin
				canRaise = p.Stack >= need
				if canRaise {
					minRaise = need
				}
			}
			canFold = true
		}

		pv := gamePlayerView{
			UserID:       p.UserID,
			Username:     p.Username,
			SeatIndex:    p.SeatIndex,
			Stack:        p.Stack,
			Folded:       p.Folded,
			LastAction:   p.LastAction,
			Won:          p.Won,
			Contributed:  p.Contributed,
			BestHandName: p.BestHandName,
			RevealMask:   p.RevealMask,
			CanReveal:    p.UserID == s.UserID && room.Game.Stage == domain.StageFinished,
			IsTurn:       isTurn,
			CanCheck:     canCheck,
			CanCall:      canCall,
			CanBet:       canBet,
			CanRaise:     canRaise,
			CanFold:      canFold,
			CallAmount:   callAmount,
			MinBet:       minBet,
			MinRaise:     minRaise,
		}
		if p.UserID == s.UserID {
			pv.HoleCards = visibleHoleCards(p.HoleCards, 3)
		} else if room.Game.Stage == domain.StageFinished {
			pv.HoleCards = visibleHoleCards(p.HoleCards, p.RevealMask)
		}
		players = append(players, pv)
	}

	resp["game"] = map[string]any{
		"stage":          room.Game.Stage,
		"pot":            room.Game.Pot,
		"dealerPos":      room.Game.DealerPos,
		"smallBlindPos":  room.Game.SmallBlindPos,
		"bigBlindPos":    room.Game.BigBlindPos,
		"turnPos":        room.Game.TurnPos,
		"communityCards": room.Game.CommunityCards,
		"players":        players,
		"result":         room.Game.Result,
		"openBetMin":     room.Game.OpenBetMin,
		"betMin":         room.Game.BetMin,
		"actionLogs":     room.Game.ActionLogs,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *GameHandler) Action(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	var req actionReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	req.Type = strings.TrimSpace(strings.ToLower(req.Type))
	if req.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action type required"})
		return
	}
	var (
		room *store.Room
		err  error
	)
	if req.Type == "reveal" {
		room, err = h.Store.ApplyReveal(roomID, s.UserID, req.ActionID, req.RevealMask, req.ExpectedVersion)
	} else {
		room, err = h.Store.ApplyAction(roomID, s.UserID, req.ActionID, req.Type, req.Amount, req.ExpectedVersion)
	}
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "version conflict" {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error(), "stateVersion": roomVersion(room)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stateVersion": room.StateVersion})
}

func visibleHoleCards(holeCards []domain.Card, revealMask int) []*domain.Card {
	visible := []*domain.Card{nil, nil}
	if len(holeCards) > 0 && (revealMask&1) != 0 {
		c := holeCards[0]
		visible[0] = &c
	}
	if len(holeCards) > 1 && (revealMask&2) != 0 {
		c := holeCards[1]
		visible[1] = &c
	}
	return visible
}

func roomVersion(r *store.Room) int64 {
	if r == nil {
		return 0
	}
	return r.StateVersion
}
