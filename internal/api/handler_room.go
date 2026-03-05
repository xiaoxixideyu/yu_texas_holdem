package api

import (
	"net/http"
	"strconv"
	"strings"

	"texas_yu/internal/store"
)

type RoomHandler struct {
	Store *store.MemoryStore
}

type createRoomReq struct {
	Name       string `json:"name"`
	OpenBetMin int    `json:"openBetMin"`
	BetMin     int    `json:"betMin"`
}

type addAIReq struct {
	Name string `json:"name"`
}

type aiManagedReq struct {
	Enabled bool `json:"enabled"`
}

type chipRefreshVoteReq struct {
	Decision string `json:"decision"`
}

func (h *RoomHandler) ListRooms(w http.ResponseWriter, r *http.Request, _ *store.Session) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	since := int64(0)
	if sv := r.URL.Query().Get("sinceVersion"); sv != "" {
		if v, err := strconv.ParseInt(sv, 10, 64); err == nil {
			since = v
		}
	}
	rooms, version := h.Store.ListRooms()
	if since > 0 && version == since {
		writeJSON(w, http.StatusOK, map[string]any{"notModified": true, "version": version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms, "version": version})
}

func (h *RoomHandler) CreateRoom(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req createRoomReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = "Room"
	}
	if req.OpenBetMin <= 0 {
		req.OpenBetMin = 10
	}
	if req.BetMin <= 0 {
		req.BetMin = 10
	}
	room := h.Store.CreateRoom(s, req.Name, req.OpenBetMin, req.BetMin)
	writeJSON(w, http.StatusOK, room)
}

func roomIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func aiUserIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 6 {
		return ""
	}
	return parts[5]
}

func (h *RoomHandler) JoinRoom(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.JoinRoom(roomID, s)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) SpectateRoom(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.SpectateRoom(roomID, s)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) StartRoom(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.StartGame(roomID, s.UserID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) LeaveRoom(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.LeaveRoom(roomID, s.UserID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if room == nil {
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) NextHand(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.NextHand(roomID, s.UserID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) AddAI(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	var req addAIReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	room, _, err := h.Store.AddAI(roomID, s.UserID, req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) RemoveAI(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	aiUserID := aiUserIDFromPath(r.URL.Path)
	if roomID == "" || aiUserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid path"})
		return
	}
	room, err := h.Store.RemoveAI(roomID, s.UserID, aiUserID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) ToggleAIManaged(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	var req aiManagedReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	room, err := h.Store.SetPlayerAIManaged(roomID, s.UserID, req.Enabled)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "spectator is read-only" || err.Error() == "user not in room" {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) StartChipRefreshVote(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	room, err := h.Store.StartChipRefreshVote(roomID, s.UserID)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "spectator is read-only" || err.Error() == "user not in room" {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandler) CastChipRefreshVote(w http.ResponseWriter, r *http.Request, s *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	roomID := roomIDFromPath(r.URL.Path)
	if roomID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid room id"})
		return
	}
	var req chipRefreshVoteReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	room, err := h.Store.CastChipRefreshVote(roomID, s.UserID, req.Decision)
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "spectator is read-only" || err.Error() == "user not in room" {
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, room)
}
