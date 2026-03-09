package store

import (
	"path/filepath"
	"testing"
)

func TestStore_StrategyParams_FileRoundTrip(t *testing.T) {
	defer setCurrentStrategyParams(defaultStrategyParams())
	path := filepath.Join(t.TempDir(), "ai_strategy.json")
	params := defaultStrategyParams()
	params.RiverTripleBarrelBonus = 0.13
	params.RiverCheckbackPairMax = 0.44
	if err := saveStrategyParamsToFile(path, params); err != nil {
		t.Fatalf("save params: %v", err)
	}
	loaded, exists, err := loadStrategyParamsFromFile(path)
	if err != nil {
		t.Fatalf("load params: %v", err)
	}
	if !exists {
		t.Fatalf("expected params file to exist")
	}
	if loaded.RiverTripleBarrelBonus != params.RiverTripleBarrelBonus {
		t.Fatalf("expected triple-barrel bonus %.2f, got %.2f", params.RiverTripleBarrelBonus, loaded.RiverTripleBarrelBonus)
	}
	if loaded.RiverCheckbackPairMax != params.RiverCheckbackPairMax {
		t.Fatalf("expected checkback pair max %.2f, got %.2f", params.RiverCheckbackPairMax, loaded.RiverCheckbackPairMax)
	}
}

func TestStore_NewMemoryStore_LoadsPersistedStrategyParams(t *testing.T) {
	defer setCurrentStrategyParams(defaultStrategyParams())
	path := filepath.Join(t.TempDir(), "ai_strategy.json")
	params := defaultStrategyParams()
	params.RiverStationPenaltyWeight = 0.61
	if err := saveStrategyParamsToFile(path, params); err != nil {
		t.Fatalf("save params: %v", err)
	}
	s := NewMemoryStore(Options{StrategyConfigPath: path})
	status := s.BenchmarkStatus()
	if status.CurrentParams.RiverStationPenaltyWeight != params.RiverStationPenaltyWeight {
		t.Fatalf("expected loaded station penalty %.2f, got %.2f", params.RiverStationPenaltyWeight, status.CurrentParams.RiverStationPenaltyWeight)
	}
	if status.ConfigPath != path {
		t.Fatalf("expected config path %s, got %s", path, status.ConfigPath)
	}
}
