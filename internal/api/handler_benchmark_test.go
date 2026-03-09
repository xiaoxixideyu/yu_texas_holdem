package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"texas_yu/internal/ai"
	"texas_yu/internal/store"
)

func TestBenchmarkHandler_StatusStartStopAndSettings(t *testing.T) {
	strategyPath := filepath.Join(t.TempDir(), "ai_strategy.json")
	runtimePath := filepath.Join(t.TempDir(), "ai_runtime.json")
	ms := store.NewMemoryStore(store.Options{
		AI:                  ai.NewService(ai.Config{APIKey: "test-key", Model: "model-a"}),
		AIConfig:            ai.Config{APIKey: "test-key", Model: "model-a", Timeout: 8 * time.Second, MaxRetry: 2},
		StrategyConfigPath:  strategyPath,
		AIRuntimeConfigPath: runtimePath,
	})
	session := ms.CreateSession("benchmark-user")
	h := &BenchmarkHandler{Store: ms}

	statusRoute := RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		h.Status(w, r, s)
	})
	startRoute := RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		h.Start(w, r, s)
	})
	stopRoute := RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		h.Stop(w, r, s)
	})
	settingsRoute := RequireSession(ms, func(w http.ResponseWriter, r *http.Request, s *store.Session) {
		h.UpdateSettings(w, r, s)
	})

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/ai-benchmark/status", nil)
	getReq.Header.Set("X-User-Id", session.UserID)
	getRec := httptest.NewRecorder()
	statusRoute(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from status, got %d", getRec.Code)
	}
	var status store.BenchmarkStatus
	if err := json.Unmarshal(getRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.ConfigPath != strategyPath {
		t.Fatalf("expected config path %s, got %s", strategyPath, status.ConfigPath)
	}
	if status.AISettings.Model != "model-a" {
		t.Fatalf("expected model-a, got %s", status.AISettings.Model)
	}
	if status.AISettings.DecisionMode != "llm" {
		t.Fatalf("expected initial llm mode, got %s", status.AISettings.DecisionMode)
	}
	if status.Running {
		t.Fatalf("expected benchmark to be idle initially")
	}

	body := bytes.NewBufferString(`{"useLlm":false,"model":"model-b"}`)
	settingsReq := httptest.NewRequest(http.MethodPost, "/api/v1/ai-benchmark/settings", body)
	settingsReq.Header.Set("X-User-Id", session.UserID)
	settingsRec := httptest.NewRecorder()
	settingsRoute(settingsRec, settingsReq)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from settings, got %d body=%s", settingsRec.Code, settingsRec.Body.String())
	}
	if err := json.Unmarshal(settingsRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode settings status: %v", err)
	}
	if status.AISettings.UseLLM {
		t.Fatalf("expected llm to be disabled")
	}
	if status.AISettings.DecisionMode != "offline" {
		t.Fatalf("expected offline mode, got %s", status.AISettings.DecisionMode)
	}
	if status.AISettings.Model != "model-b" {
		t.Fatalf("expected model-b, got %s", status.AISettings.Model)
	}

	startReq := httptest.NewRequest(http.MethodPost, "/api/v1/ai-benchmark/start", nil)
	startReq.Header.Set("X-User-Id", session.UserID)
	startRec := httptest.NewRecorder()
	startRoute(startRec, startReq)
	if startRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from start, got %d body=%s", startRec.Code, startRec.Body.String())
	}
	if err := json.Unmarshal(startRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode start status: %v", err)
	}
	if !status.Running {
		t.Fatalf("expected benchmark to be running after start")
	}

	time.Sleep(20 * time.Millisecond)
	stopReq := httptest.NewRequest(http.MethodPost, "/api/v1/ai-benchmark/stop", nil)
	stopReq.Header.Set("X-User-Id", session.UserID)
	stopRec := httptest.NewRecorder()
	stopRoute(stopRec, stopReq)
	if stopRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from stop, got %d", stopRec.Code)
	}
	if err := json.Unmarshal(stopRec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode stop status: %v", err)
	}
	if status.Running {
		t.Fatalf("expected benchmark to stop")
	}
}
