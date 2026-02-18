package api

import (
	"net/http"
	"strings"

	"texas_yu/internal/store"
)

type AuthHandler struct {
	Store *store.MemoryStore
}

type createSessionReq struct {
	Username string `json:"username"`
}

func (h *AuthHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req createSessionReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "username required"})
		return
	}
	s := h.Store.CreateSession(req.Username)
	writeJSON(w, http.StatusOK, s)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	RequireSession(h.Store, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		writeJSON(w, http.StatusOK, s)
	})(w, r)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	RequireSession(h.Store, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		h.Store.RemoveUser(s.UserID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})(w, r)
}
