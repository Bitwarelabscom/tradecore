package receiver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"tradecore/internal/types"
)

func TestHTTPReceiver_HandleHealth(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eventChan := make(chan types.Event, 1)
	receiver := NewHTTPReceiver(8080, eventChan, logger)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	receiver.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.Status)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if body["status"] != "healthy" {
		t.Errorf("Expected status healthy, got %v", body["status"])
	}
}

func TestHTTPReceiver_HandleSignal(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eventChan := make(chan types.Event, 1)
	receiver := NewHTTPReceiver(8080, eventChan, logger)

	// Valid signal
	validSignal := SignalRequest{
		Symbol: "BTCUSDT",
		Action: "START_GRID",
		GridConfig: &types.GridConfig{
			UpperPrice: 50000,
			LowerPrice: 40000,
			GridLines:  10,
			Quantity:   0.001,
		},
	}
	body, _ := json.Marshal(validSignal)
	req := httptest.NewRequest(http.MethodPost, "/signal", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Handle in a goroutine because it writes to a channel
	go receiver.handleSignal(w, req)

	// Read from channel
	select {
	case event := <-eventChan:
		if event.Signal.Symbol != "BTCUSDT" {
			t.Errorf("Expected symbol BTCUSDT, got %v", event.Signal.Symbol)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for event")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.Status)
	}
}

func TestHTTPReceiver_HandleSignal_Invalid(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eventChan := make(chan types.Event, 1)
	receiver := NewHTTPReceiver(8080, eventChan, logger)

	// Invalid signal (missing config)
	invalidSignal := SignalRequest{
		Symbol: "BTCUSDT",
		Action: "START_GRID",
	}
	body, _ := json.Marshal(invalidSignal)
	req := httptest.NewRequest(http.MethodPost, "/signal", bytes.NewReader(body))
	w := httptest.NewRecorder()

	receiver.handleSignal(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status BadRequest, got %v", resp.Status)
	}
}

func TestHTTPReceiver_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	eventChan := make(chan types.Event, 1)
	// Use a random high port to avoid conflicts
	receiver := NewHTTPReceiver(48123, eventChan, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	err := receiver.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Make a request to ensure it's running
	resp, err := http.Get("http://127.0.0.1:48123/health")
	if err != nil {
		t.Errorf("Failed to make request: %v", err)
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Stop server
	if err := receiver.Stop(ctx); err != nil {
		t.Errorf("Failed to stop server: %v", err)
	}
}
