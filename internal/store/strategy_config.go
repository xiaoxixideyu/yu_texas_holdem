package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

const defaultStrategyConfigPath = "data/ai_strategy.json"

type StrategyParams struct {
	FlopDryRangeBetReduction       float64 `json:"flopDryRangeBetReduction"`
	TurnScareSizingWeight          float64 `json:"turnScareSizingWeight"`
	RiverMissedDrawSizingWeight    float64 `json:"riverMissedDrawSizingWeight"`
	RiverMissedDrawBluffWeight     float64 `json:"riverMissedDrawBluffWeight"`
	RiverShowdownPenaltyWeight     float64 `json:"riverShowdownPenaltyWeight"`
	RiverStationPenaltyWeight      float64 `json:"riverStationPenaltyWeight"`
	RiverTripleBarrelBonus         float64 `json:"riverTripleBarrelBonus"`
	RiverCheckbackStationThreshold float64 `json:"riverCheckbackStationThreshold"`
	RiverCheckbackPairMax          float64 `json:"riverCheckbackPairMax"`
	RiverThinValueStationPenalty   float64 `json:"riverThinValueStationPenalty"`
	RiverStealMissedDrawWeight     float64 `json:"riverStealMissedDrawWeight"`
	RiverStealShowdownPenalty      float64 `json:"riverStealShowdownPenalty"`
	RiverStealStationPenalty       float64 `json:"riverStealStationPenalty"`
}

type strategyConfigFile struct {
	Version   int            `json:"version"`
	UpdatedAt string         `json:"updatedAt"`
	Params    StrategyParams `json:"params"`
}

var strategyParamsValue atomic.Value

func init() {
	strategyParamsValue.Store(defaultStrategyParams())
}

func defaultStrategyParams() StrategyParams {
	return StrategyParams{
		FlopDryRangeBetReduction:       0.05,
		TurnScareSizingWeight:          0.20,
		RiverMissedDrawSizingWeight:    0.18,
		RiverMissedDrawBluffWeight:     0.42,
		RiverShowdownPenaltyWeight:     0.36,
		RiverStationPenaltyWeight:      0.45,
		RiverTripleBarrelBonus:         0.06,
		RiverCheckbackStationThreshold: 0.10,
		RiverCheckbackPairMax:          0.50,
		RiverThinValueStationPenalty:   0.18,
		RiverStealMissedDrawWeight:     0.40,
		RiverStealShowdownPenalty:      0.34,
		RiverStealStationPenalty:       0.42,
	}
}

func clampStrategyParams(params StrategyParams) StrategyParams {
	params.FlopDryRangeBetReduction = clampFloat(params.FlopDryRangeBetReduction, 0, 0.12)
	params.TurnScareSizingWeight = clampFloat(params.TurnScareSizingWeight, 0.05, 0.40)
	params.RiverMissedDrawSizingWeight = clampFloat(params.RiverMissedDrawSizingWeight, 0, 0.35)
	params.RiverMissedDrawBluffWeight = clampFloat(params.RiverMissedDrawBluffWeight, 0.05, 0.70)
	params.RiverShowdownPenaltyWeight = clampFloat(params.RiverShowdownPenaltyWeight, 0.05, 0.70)
	params.RiverStationPenaltyWeight = clampFloat(params.RiverStationPenaltyWeight, 0.05, 0.80)
	params.RiverTripleBarrelBonus = clampFloat(params.RiverTripleBarrelBonus, 0, 0.20)
	params.RiverCheckbackStationThreshold = clampFloat(params.RiverCheckbackStationThreshold, 0.04, 0.24)
	params.RiverCheckbackPairMax = clampFloat(params.RiverCheckbackPairMax, 0.25, 0.70)
	params.RiverThinValueStationPenalty = clampFloat(params.RiverThinValueStationPenalty, 0, 0.40)
	params.RiverStealMissedDrawWeight = clampFloat(params.RiverStealMissedDrawWeight, 0.05, 0.70)
	params.RiverStealShowdownPenalty = clampFloat(params.RiverStealShowdownPenalty, 0.05, 0.70)
	params.RiverStealStationPenalty = clampFloat(params.RiverStealStationPenalty, 0.05, 0.80)
	return params
}

func currentStrategyParams() StrategyParams {
	if loaded := strategyParamsValue.Load(); loaded != nil {
		if params, ok := loaded.(StrategyParams); ok {
			return clampStrategyParams(params)
		}
	}
	return defaultStrategyParams()
}

func setCurrentStrategyParams(params StrategyParams) {
	strategyParamsValue.Store(clampStrategyParams(params))
}

func strategyConfigPath(path string) string {
	if path == "" {
		return defaultStrategyConfigPath
	}
	return path
}

func loadStrategyParamsFromFile(path string) (StrategyParams, bool, error) {
	path = strategyConfigPath(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultStrategyParams(), false, nil
		}
		return defaultStrategyParams(), false, err
	}
	var cfg strategyConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaultStrategyParams(), false, err
	}
	return clampStrategyParams(cfg.Params), true, nil
}

func saveStrategyParamsToFile(path string, params StrategyParams) error {
	path = strategyConfigPath(path)
	params = clampStrategyParams(params)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := strategyConfigFile{
		Version:   1,
		UpdatedAt: time.Now().Format(time.RFC3339),
		Params:    params,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
