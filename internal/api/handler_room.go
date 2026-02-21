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
