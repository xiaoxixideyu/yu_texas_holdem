package main

import (
	"log"
	"net/http"
	"strings"

	"texas_yu/internal/api"
	"texas_yu/internal/store"
)

func main() {
	ms := store.NewMemoryStore()
	authH := &api.AuthHandler{Store: ms}
	roomH := &api.RoomHandler{Store: ms}
	gameH := &api.GameHandler{Store: ms}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/session", authH.CreateSession)
	mux.HandleFunc("/api/v1/session/me", authH.Me)
	mux.HandleFunc("/api/v1/session/logout", authH.Logout)

	mux.HandleFunc("/api/v1/rooms", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		switch r.Method {
		case http.MethodGet:
			roomH.ListRooms(w, r, s)
		case http.MethodPost:
			roomH.CreateRoom(w, r, s)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/v1/rooms/", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/rooms/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		action := parts[1]
		switch action {
		case "join":
			roomH.JoinRoom(w, r, s)
		case "start":
			roomH.StartRoom(w, r, s)
		case "leave":
			roomH.LeaveRoom(w, r, s)
		case "next-hand":
			roomH.NextHand(w, r, s)
		case "state":
			gameH.GetState(w, r, s)
		case "actions":
			gameH.Action(w, r, s)
		default:
			http.NotFound(w, r)
		}
	}))

	fs := http.FileServer(http.Dir("web/static"))
	mux.Handle("/", fs)

	addr := ":8080"
	log.Printf("server started at http://localhost%s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
