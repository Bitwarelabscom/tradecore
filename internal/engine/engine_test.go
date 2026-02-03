package engine

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tradecore/internal/exchange"
	"tradecore/internal/types"
)

func TestEngine_GridBot_Lifecycle(t *testing.T) {
	// Setup
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mockExec := exchange.NewMockExecutor(logger)
	mockStreamer := exchange.NewMockMarketStreamer(logger, exchange.WithTickerSpeed(time.Hour))

	// Temp state file
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "test_state.json")
	persistence := NewStatePersistence(stateFile, logger)

	// Create engine
	eng := NewEngine(mockExec, mockStreamer, persistence, logger)

	// Start engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := eng.Start(ctx); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	// 1. Send START_GRID signal
	startSignal := &types.SignalEvent{
		Symbol: "BTCUSDT",
		Action: types.ActionStartGrid,
		GridConfig: &types.GridConfig{
			UpperPrice: 55000,
			LowerPrice: 45000,
			GridLines:  5, // Step = 2000. Levels: 45000, 47000, 49000, 51000, 53000, 55000
			Quantity:   0.001,
		},
		Timestamp: time.Now(),
	}

	eng.InputChannel() <- types.Event{
		Type:   types.EventTypeSignal,
		Signal: startSignal,
	}

	// Allow time for processing
	time.Sleep(100 * time.Millisecond)

	// Verify bot started
	bots := eng.GetActiveBots()
	if _, ok := bots["BTCUSDT"]; !ok {
		t.Fatal("Bot BTCUSDT should be active")
	}
	grid := bots["BTCUSDT"].Grid
	if grid == nil || !grid.Active {
		t.Fatal("Grid strategy should be active")
	}

	// 2. Trigger BUY
	// Current price is ~50000 (from setup or default?)
	// Let's set initial price to 50000
	mockStreamer.InjectPrice("BTCUSDT", 50000)
	time.Sleep(50 * time.Millisecond)

	// Move price down to cross 49000
	// 49000 is a level. Crossing down through it triggers BUY.
	// Last price 50000. Current 48900.
	mockStreamer.InjectPrice("BTCUSDT", 48900)
	time.Sleep(100 * time.Millisecond)

	// Check if buy order executed
	orders := mockExec.GetOrders()
	if len(orders) == 0 {
		t.Fatal("Expected buy order execution")
	}
	lastOrder := orders[len(orders)-1]
	if lastOrder.Side != types.SideBuy {
		t.Errorf("Expected BUY order, got %v", lastOrder.Side)
	}
	if lastOrder.Price != 49000 { // Level price
		t.Errorf("Expected price 49000, got %v", lastOrder.Price)
	}

	// 3. Trigger SELL
	// Move price up through 49000 again.
	// Last price 48900. Current 49100.
	mockStreamer.InjectPrice("BTCUSDT", 49100)
	time.Sleep(100 * time.Millisecond)

	// Check if sell order executed
	orders = mockExec.GetOrders()
	if len(orders) < 2 {
		t.Fatal("Expected sell order execution")
	}
	lastOrder = orders[len(orders)-1]
	if lastOrder.Side != types.SideSell {
		t.Errorf("Expected SELL order, got %v", lastOrder.Side)
	}
	if lastOrder.Price != 49000 {
		t.Errorf("Expected price 49000, got %v", lastOrder.Price)
	}

	// 4. Send STOP_GRID signal
	stopSignal := &types.SignalEvent{
		Symbol: "BTCUSDT",
		Action: types.ActionStopGrid,
	}
	eng.InputChannel() <- types.Event{
		Type:   types.EventTypeSignal,
		Signal: stopSignal,
	}

	time.Sleep(100 * time.Millisecond)

	// Verify stopped
	bots = eng.GetActiveBots()
	grid = bots["BTCUSDT"].Grid
	if grid.Active {
		t.Error("Grid strategy should be inactive")
	}

	// Stop engine
	eng.Stop()
}
