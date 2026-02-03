package engine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tradecore/internal/types"
)

const (
	stateVersion    = 1
	defaultStateFile = "./state.json"
)

// StatePersistence handles saving and loading bot state
type StatePersistence struct {
	filePath    string
	logger      *slog.Logger
	mu          sync.Mutex
	lastSave    time.Time
	saveInterval time.Duration
	dirty       bool
}

// NewStatePersistence creates a new state persistence handler
func NewStatePersistence(filePath string, logger *slog.Logger) *StatePersistence {
	if filePath == "" {
		filePath = defaultStateFile
	}

	return &StatePersistence{
		filePath:     filePath,
		logger:       logger,
		saveInterval: 30 * time.Second,
	}
}

// Load reads the state from disk
func (p *StatePersistence) Load() (map[string]*types.BotState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := os.ReadFile(p.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			p.logger.Info("[PERSISTENCE] No existing state file, starting fresh",
				"path", p.filePath,
			)
			return make(map[string]*types.BotState), nil
		}
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var snapshot types.StateSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	// Version check
	if snapshot.Version != stateVersion {
		p.logger.Warn("[PERSISTENCE] State version mismatch, starting fresh",
			"file_version", snapshot.Version,
			"expected_version", stateVersion,
		)
		return make(map[string]*types.BotState), nil
	}

	p.logger.Info("[PERSISTENCE] State loaded",
		"path", p.filePath,
		"bots", len(snapshot.ActiveBots),
		"saved_at", snapshot.SavedAt.Format(time.RFC3339),
	)

	return snapshot.ActiveBots, nil
}

// Save writes the state to disk atomically
func (p *StatePersistence) Save(bots map[string]*types.BotState) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.saveUnsafe(bots)
}

// saveUnsafe performs the actual save without locking (caller must hold lock)
func (p *StatePersistence) saveUnsafe(bots map[string]*types.BotState) error {
	snapshot := types.StateSnapshot{
		ActiveBots: bots,
		SavedAt:    time.Now(),
		Version:    stateVersion,
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Create directory if needed
	dir := filepath.Dir(p.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Write to temp file first (atomic write)
	tempFile := p.filePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state file: %w", err)
	}

	// Rename temp to actual (atomic on most filesystems)
	if err := os.Rename(tempFile, p.filePath); err != nil {
		os.Remove(tempFile) // Clean up temp file
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	p.lastSave = time.Now()
	p.dirty = false

	p.logger.Debug("[PERSISTENCE] State saved",
		"path", p.filePath,
		"bots", len(bots),
	)

	return nil
}

// MarkDirty marks the state as needing to be saved
func (p *StatePersistence) MarkDirty() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dirty = true
}

// ShouldSave returns true if enough time has passed since last save and state is dirty
func (p *StatePersistence) ShouldSave() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dirty && time.Since(p.lastSave) >= p.saveInterval
}

// ForceSave saves the state regardless of the dirty flag or interval
func (p *StatePersistence) ForceSave(bots map[string]*types.BotState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.saveUnsafe(bots)
}

// StartPeriodicSave starts a goroutine that periodically saves state
func (p *StatePersistence) StartPeriodicSave(bots func() map[string]*types.BotState, stopChan <-chan struct{}) {
	ticker := time.NewTicker(p.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			// Final save on shutdown
			if err := p.ForceSave(bots()); err != nil {
				p.logger.Error("[PERSISTENCE] Failed to save state on shutdown",
					"error", err,
				)
			} else {
				p.logger.Info("[PERSISTENCE] Final state saved on shutdown")
			}
			return
		case <-ticker.C:
			if p.ShouldSave() {
				if err := p.Save(bots()); err != nil {
					p.logger.Error("[PERSISTENCE] Failed to save state",
						"error", err,
					)
				}
			}
		}
	}
}

// Delete removes the state file
func (p *StatePersistence) Delete() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.Remove(p.filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete state file: %w", err)
	}

	p.logger.Info("[PERSISTENCE] State file deleted", "path", p.filePath)
	return nil
}

// GetFilePath returns the state file path
func (p *StatePersistence) GetFilePath() string {
	return p.filePath
}
