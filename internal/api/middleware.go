package api

import (
	"net/http"
	"strings"

	"texas_yu/internal/store"
)

func RequireSession(ms *store.MemoryStore, next func(http.ResponseWriter, *http.Request, *store.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-Id")
		if userID == "" {
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				userID = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if userID == "" {
			userID = r.URL.Query().Get("userId")
		}

		s, ok := ms.GetUser(userID)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		ms.TouchUser(s.UserID)
		next(w, r, s)
	}
}
