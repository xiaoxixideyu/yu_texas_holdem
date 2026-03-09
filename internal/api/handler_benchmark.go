package api

import (
	"net/http"

	"texas_yu/internal/store"
)

type BenchmarkHandler struct {
	Store *store.MemoryStore
}

func (h *BenchmarkHandler) Status(w http.ResponseWriter, r *http.Request, _ *store.Session) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, h.Store.BenchmarkStatus())
}

func (h *BenchmarkHandler) Start(w http.ResponseWriter, r *http.Request, _ *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	status, err := h.Store.StartBenchmark()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": h.Store.BenchmarkStatus()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *BenchmarkHandler) Stop(w http.ResponseWriter, r *http.Request, _ *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, h.Store.StopBenchmark())
}

type updateAIRuntimeReq struct {
	UseLLM bool   `json:"useLlm"`
	Model  string `json:"model"`
}

func (h *BenchmarkHandler) UpdateSettings(w http.ResponseWriter, r *http.Request, _ *store.Session) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	var req updateAIRuntimeReq
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}
	if _, err := h.Store.UpdateAIRuntimeSettings(store.AIRuntimeSettings{UseLLM: req.UseLLM, Model: req.Model}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": h.Store.BenchmarkStatus()})
		return
	}
	writeJSON(w, http.StatusOK, h.Store.BenchmarkStatus())
}
