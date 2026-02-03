package engine

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tradecore/internal/types"
)

func TestStatePersistence_SaveAndLoad(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "persistence_test.json")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	
	p := NewStatePersistence(stateFile, logger)

	// Create some state
	bots := make(map[string]*types.BotState)
	bots["BTCUSDT"] = &types.BotState{
		BotID:  "bot-1",
		Symbol: "BTCUSDT",
		Grid: &types.GridState{
			Active:    true,
			LastPrice: 50000,
		},
		UpdatedAt: time.Now(),
	}

	// Save
	if err := p.ForceSave(bots); err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("State file was not created")
	}

	// Load
	loadedBots, err := p.Load()
	if err != nil {
		t.Fatalf("Failed to load state: %v", err)
	}

	// Verify content
	if len(loadedBots) != 1 {
		t.Errorf("Expected 1 bot, got %d", len(loadedBots))
	}
	
	loadedBot, ok := loadedBots["BTCUSDT"]
	if !ok {
		t.Fatal("Bot BTCUSDT not found in loaded state")
	}
	
	if loadedBot.BotID != "bot-1" {
		t.Errorf("Expected bot ID bot-1, got %s", loadedBot.BotID)
	}
	
	if !loadedBot.Grid.Active {
		t.Error("Expected grid to be active")
	}
	
	if loadedBot.Grid.LastPrice != 50000 {
		t.Errorf("Expected last price 50000, got %f", loadedBot.Grid.LastPrice)
	}
}

func TestStatePersistence_LoadNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "non_existent.json")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	p := NewStatePersistence(stateFile, logger)

	bots, err := p.Load()
	if err != nil {
		t.Fatalf("Load failed for non-existent file: %v", err)
	}

	if len(bots) != 0 {
		t.Errorf("Expected empty map, got %d items", len(bots))
	}
}
