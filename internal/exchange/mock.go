package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"tradecore/internal/types"
)

// MockExecutor implements Executor for testing without real trades
type MockExecutor struct {
	logger      *slog.Logger
	mu          sync.RWMutex
	balances    map[string]float64
	orders      []types.OrderRequest
	orderIDSeq  atomic.Int64
	fillDelay   time.Duration
	shouldFail  bool
	failMessage string
}

// MockExecutorOption configures the mock executor
type MockExecutorOption func(*MockExecutor)

// WithMockBalance sets initial balance for an asset
func WithMockBalance(asset string, amount float64) MockExecutorOption {
	return func(m *MockExecutor) {
		m.balances[asset] = amount
	}
}

// WithFillDelay simulates order fill delay
func WithFillDelay(d time.Duration) MockExecutorOption {
	return func(m *MockExecutor) {
		m.fillDelay = d
	}
}

// WithFailure makes the mock executor fail orders
func WithFailure(msg string) MockExecutorOption {
	return func(m *MockExecutor) {
		m.shouldFail = true
		m.failMessage = msg
	}
}

// NewMockExecutor creates a new mock executor for testing
func NewMockExecutor(logger *slog.Logger, opts ...MockExecutorOption) *MockExecutor {
	m := &MockExecutor{
		logger:   logger,
		balances: make(map[string]float64),
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Set default balances if none provided
	if len(m.balances) == 0 {
		m.balances["USDT"] = 10000.0
		m.balances["BTC"] = 1.0
		m.balances["ETH"] = 10.0
	}

	return m
}

// ExecuteOrder simulates order execution by logging
func (m *MockExecutor) ExecuteOrder(ctx context.Context, req types.OrderRequest) (*types.OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for configured failure
	if m.shouldFail {
		m.logger.Error("[MOCK] Order failed (configured)",
			"symbol", req.Symbol,
			"side", req.Side,
			"error", m.failMessage,
		)
		return &types.OrderResult{
			Success: false,
			Error:   fmt.Errorf("%s", m.failMessage),
		}, nil
	}

	// Simulate fill delay if configured
	if m.fillDelay > 0 {
		select {
		case <-time.After(m.fillDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Generate order ID
	orderID := fmt.Sprintf("MOCK-%d", m.orderIDSeq.Add(1))

	// Record the order
	m.orders = append(m.orders, req)

	// Log the simulated order
	m.logger.Info("[MOCK] Order executed",
		"order_id", orderID,
		"symbol", req.Symbol,
		"side", req.Side,
		"quantity", req.Quantity,
		"price", req.Price,
	)

	// Simulate balance changes
	m.simulateBalanceChange(req)

	return &types.OrderResult{
		Success:   true,
		OrderID:   orderID,
		FilledQty: req.Quantity,
		AvgPrice:  req.Price,
	}, nil
}

// simulateBalanceChange updates mock balances based on the order
func (m *MockExecutor) simulateBalanceChange(req types.OrderRequest) {
	// Extract base and quote assets from symbol (e.g., BTCUSDT -> BTC, USDT)
	base, quote := parseSymbol(req.Symbol)

	cost := req.Quantity * req.Price

	if req.Side == types.SideBuy {
		// Deduct quote, add base
		m.balances[quote] -= cost
		m.balances[base] += req.Quantity
	} else {
		// Add quote, deduct base
		m.balances[quote] += cost
		m.balances[base] -= req.Quantity
	}
}

// parseSymbol extracts base and quote assets from a trading pair
func parseSymbol(symbol string) (base, quote string) {
	// Common quote currencies
	quotes := []string{"USDT", "BUSD", "USD", "BTC", "ETH", "BNB"}

	for _, q := range quotes {
		if len(symbol) > len(q) && symbol[len(symbol)-len(q):] == q {
			return symbol[:len(symbol)-len(q)], q
		}
	}

	// Default fallback
	if len(symbol) > 4 {
		return symbol[:len(symbol)-4], symbol[len(symbol)-4:]
	}
	return symbol, "USDT"
}

// GetBalance returns the mock balance for an asset
func (m *MockExecutor) GetBalance(ctx context.Context, asset string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	balance, ok := m.balances[asset]
	if !ok {
		return 0, nil
	}
	return balance, nil
}

// PlaceOrder implements Executor interface using exchange types
func (m *MockExecutor) PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for configured failure
	if m.shouldFail {
		m.logger.Error("[MOCK] Order failed (configured)",
			"symbol", req.Symbol,
			"side", req.Side,
			"error", m.failMessage,
		)
		return nil, fmt.Errorf("%s", m.failMessage)
	}

	// Simulate fill delay if configured
	if m.fillDelay > 0 {
		select {
		case <-time.After(m.fillDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Generate order ID
	orderID := fmt.Sprintf("MOCK-%d", m.orderIDSeq.Add(1))

	// Log the simulated order
	m.logger.Info("[MOCK] PlaceOrder executed",
		"order_id", orderID,
		"symbol", req.Symbol,
		"side", req.Side,
		"quantity", req.Quantity,
		"quote_qty", req.QuoteOrderQty,
	)

	return &OrderResult{
		OrderID:     orderID,
		Symbol:      req.Symbol,
		Status:      "FILLED",
		ExecutedQty: req.Quantity,
		Price:       "0", // Mock doesn't track real price
	}, nil
}

// Close is a no-op for the mock executor
func (m *MockExecutor) Close() error {
	m.logger.Info("[MOCK] Executor closed")
	return nil
}

// GetOrders returns all recorded orders (for testing)
func (m *MockExecutor) GetOrders() []types.OrderRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	orders := make([]types.OrderRequest, len(m.orders))
	copy(orders, m.orders)
	return orders
}

// ClearOrders clears the order history (for testing)
func (m *MockExecutor) ClearOrders() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = nil
}

// SetBalance sets a mock balance (for testing)
func (m *MockExecutor) SetBalance(asset string, amount float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balances[asset] = amount
}

// MockMarketStreamer implements MarketStreamer for testing
type MockMarketStreamer struct {
	logger      *slog.Logger
	mu          sync.RWMutex
	eventChan   chan types.Event
	symbols     map[string]bool
	stopChans   map[string]chan struct{}
	priceBase   map[string]float64
	volatility  float64
	tickerSpeed time.Duration
	closed      bool
}

// MockStreamerOption configures the mock streamer
type MockStreamerOption func(*MockMarketStreamer)

// WithTickerSpeed sets how often price updates are generated
func WithTickerSpeed(d time.Duration) MockStreamerOption {
	return func(m *MockMarketStreamer) {
		m.tickerSpeed = d
	}
}

// WithVolatility sets price movement volatility (percentage)
func WithVolatility(v float64) MockStreamerOption {
	return func(m *MockMarketStreamer) {
		m.volatility = v
	}
}

// WithBasePrice sets the starting price for a symbol
func WithBasePrice(symbol string, price float64) MockStreamerOption {
	return func(m *MockMarketStreamer) {
		m.priceBase[symbol] = price
	}
}

// NewMockMarketStreamer creates a mock market data streamer
func NewMockMarketStreamer(logger *slog.Logger, opts ...MockStreamerOption) *MockMarketStreamer {
	m := &MockMarketStreamer{
		logger:      logger,
		eventChan:   make(chan types.Event, 1000),
		symbols:     make(map[string]bool),
		stopChans:   make(map[string]chan struct{}),
		priceBase:   make(map[string]float64),
		volatility:  0.001, // 0.1% default volatility
		tickerSpeed: 100 * time.Millisecond,
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	// Set default prices
	if _, ok := m.priceBase["BTCUSDT"]; !ok {
		m.priceBase["BTCUSDT"] = 65000.0
	}
	if _, ok := m.priceBase["ETHUSDT"]; !ok {
		m.priceBase["ETHUSDT"] = 3500.0
	}

	return m
}

// Subscribe starts generating mock price updates for a symbol
func (m *MockMarketStreamer) Subscribe(ctx context.Context, symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("streamer is closed")
	}

	if m.symbols[symbol] {
		return nil // Already subscribed
	}

	m.symbols[symbol] = true
	stopChan := make(chan struct{})
	m.stopChans[symbol] = stopChan

	// Get base price
	basePrice := m.priceBase[symbol]
	if basePrice == 0 {
		basePrice = 100.0 // Default
		m.priceBase[symbol] = basePrice
	}

	// Start price generator goroutine
	go m.generatePrices(ctx, symbol, basePrice, stopChan)

	m.logger.Info("[MOCK] Subscribed to symbol", "symbol", symbol)
	return nil
}

// generatePrices creates simulated price movements
func (m *MockMarketStreamer) generatePrices(ctx context.Context, symbol string, basePrice float64, stopChan chan struct{}) {
	ticker := time.NewTicker(m.tickerSpeed)
	defer ticker.Stop()

	currentPrice := basePrice
	direction := 1.0

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopChan:
			return
		case <-ticker.C:
			// Simple random walk with mean reversion
			change := currentPrice * m.volatility * direction

			// Add some randomness to direction
			if currentPrice > basePrice*1.02 {
				direction = -1.0
			} else if currentPrice < basePrice*0.98 {
				direction = 1.0
			} else {
				// Random flip
				if time.Now().UnixNano()%3 == 0 {
					direction *= -1
				}
			}

			currentPrice += change

			event := types.Event{
				Type: types.EventTypePriceUpdate,
				PriceUpdate: &types.PriceUpdate{
					Symbol:    symbol,
					Price:     currentPrice,
					Quantity:  0.1,
					Timestamp: time.Now(),
				},
			}

			select {
			case m.eventChan <- event:
			default:
				// Channel full, skip this update
			}
		}
	}
}

// Unsubscribe stops generating price updates for a symbol
func (m *MockMarketStreamer) Unsubscribe(symbol string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.symbols[symbol] {
		return nil
	}

	if stopChan, ok := m.stopChans[symbol]; ok {
		close(stopChan)
		delete(m.stopChans, symbol)
	}
	delete(m.symbols, symbol)

	m.logger.Info("[MOCK] Unsubscribed from symbol", "symbol", symbol)
	return nil
}

// Events returns the channel for receiving price updates
func (m *MockMarketStreamer) Events() <-chan types.Event {
	return m.eventChan
}

// GetPrice returns current mock price for a symbol
func (m *MockMarketStreamer) GetPrice(ctx context.Context, symbol string) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if price, ok := m.priceBase[symbol]; ok {
		// Add small random variation
		variation := price * 0.001 * float64(time.Now().UnixNano()%100-50) / 50
		return price + variation, nil
	}

	return 0, fmt.Errorf("symbol %s not found", symbol)
}

// GetKlines returns mock historical kline data
func (m *MockMarketStreamer) GetKlines(ctx context.Context, symbol string, interval string, limit int) ([]Kline, error) {
	m.mu.RLock()
	basePrice := m.priceBase[symbol]
	m.mu.RUnlock()

	if basePrice == 0 {
		basePrice = 100.0
	}

	// Generate mock klines
	klines := make([]Kline, limit)
	now := time.Now()

	// Parse interval to duration
	intervalDuration := 15 * time.Minute // Default
	switch interval {
	case "1m":
		intervalDuration = time.Minute
	case "5m":
		intervalDuration = 5 * time.Minute
	case "15m":
		intervalDuration = 15 * time.Minute
	case "1h":
		intervalDuration = time.Hour
	case "4h":
		intervalDuration = 4 * time.Hour
	case "1d":
		intervalDuration = 24 * time.Hour
	}

	for i := 0; i < limit; i++ {
		idx := limit - 1 - i
		openTime := now.Add(-time.Duration(i+1) * intervalDuration)
		closeTime := now.Add(-time.Duration(i) * intervalDuration)

		// Generate realistic OHLCV with some variation
		variation := basePrice * 0.02 * float64((i*7)%100-50) / 50
		open := basePrice + variation
		high := open * (1 + 0.005)
		low := open * (1 - 0.005)
		close := open * (1 + 0.002*float64((i*13)%100-50)/50)
		volume := 1000.0 + float64((i*17)%500)

		klines[idx] = Kline{
			OpenTime:  openTime.UnixMilli(),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: closeTime.UnixMilli(),
		}
	}

	return klines, nil
}

// Close stops all price generators
func (m *MockMarketStreamer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	for symbol, stopChan := range m.stopChans {
		close(stopChan)
		delete(m.stopChans, symbol)
		delete(m.symbols, symbol)
	}

	close(m.eventChan)
	m.logger.Info("[MOCK] Market streamer closed")
	return nil
}

// InjectPrice allows tests to inject specific price updates
func (m *MockMarketStreamer) InjectPrice(symbol string, price float64) {
	event := types.Event{
		Type: types.EventTypePriceUpdate,
		PriceUpdate: &types.PriceUpdate{
			Symbol:    symbol,
			Price:     price,
			Quantity:  1.0,
			Timestamp: time.Now(),
		},
	}

	select {
	case m.eventChan <- event:
	default:
	}
}
