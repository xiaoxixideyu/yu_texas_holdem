package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"texas_yu/internal/ai"
	"texas_yu/internal/api"
	"texas_yu/internal/store"
)

func main() {
	aiCfg := ai.LoadConfigFromEnv()
	aiSvc := ai.NewService(aiCfg)
	if aiSvc.Enabled() {
		log.Printf("AI enabled: model=%s format=%s baseURL=%s timeout=%s maxRetry=%d", aiCfg.Model, aiCfg.APIFormat, aiCfg.BaseURL, aiCfg.Timeout, aiCfg.MaxRetry)
	} else {
		log.Printf("AI disabled: set AI_API_KEY and AI_MODEL to enable")
	}

	strategyConfigPath := strings.TrimSpace(os.Getenv("AI_STRATEGY_CONFIG_PATH"))
	if strategyConfigPath == "" {
		strategyConfigPath = "data/ai_strategy.json"
	}
	aiRuntimeConfigPath := strings.TrimSpace(os.Getenv("AI_RUNTIME_CONFIG_PATH"))
	if aiRuntimeConfigPath == "" {
		aiRuntimeConfigPath = "data/ai_runtime.json"
	}
	ms := store.NewMemoryStore(store.Options{AI: aiSvc, AIConfig: aiCfg, StrategyConfigPath: strategyConfigPath, AIRuntimeConfigPath: aiRuntimeConfigPath})
	authH := &api.AuthHandler{Store: ms}
	roomH := &api.RoomHandler{Store: ms}
	gameH := &api.GameHandler{Store: ms}
	benchmarkH := &api.BenchmarkHandler{Store: ms}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/session", authH.CreateSession)
	mux.HandleFunc("/api/v1/session/me", authH.Me)
	mux.HandleFunc("/api/v1/session/logout", authH.Logout)

	mux.HandleFunc("/api/v1/ai-benchmark/status", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		benchmarkH.Status(w, r, s)
	}))
	mux.HandleFunc("/api/v1/ai-benchmark/start", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		benchmarkH.Start(w, r, s)
	}))
	mux.HandleFunc("/api/v1/ai-benchmark/stop", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		benchmarkH.Stop(w, r, s)
	}))
	mux.HandleFunc("/api/v1/ai-benchmark/settings", api.RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		benchmarkH.UpdateSettings(w, r, s)
	}))
	mux.HandleFunc("/ai_benchmark", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/static/ai_benchmark.html")
	})

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
		case "spectate":
			roomH.SpectateRoom(w, r, s)
		case "start":
			roomH.StartRoom(w, r, s)
		case "leave":
			roomH.LeaveRoom(w, r, s)
		case "next-hand":
			roomH.NextHand(w, r, s)
		case "ai":
			if len(parts) == 2 && r.Method == http.MethodPost {
				roomH.AddAI(w, r, s)
				return
			}
			if len(parts) == 3 && r.Method == http.MethodDelete {
				roomH.RemoveAI(w, r, s)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "ai-managed":
			roomH.ToggleAIManaged(w, r, s)
		case "chip-refresh":
			if len(parts) == 2 && r.Method == http.MethodPost {
				roomH.StartChipRefreshVote(w, r, s)
				return
			}
			if len(parts) == 3 && parts[2] == "vote" && r.Method == http.MethodPost {
				roomH.CastChipRefreshVote(w, r, s)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "state":
			gameH.GetState(w, r, s)
		case "actions":
			gameH.Action(w, r, s)
		case "quick-chats":
			if r.Method == http.MethodGet {
				gameH.GetQuickChats(w, r, s)
				return
			}
			if r.Method == http.MethodPost {
				gameH.QuickChat(w, r, s)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
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
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
