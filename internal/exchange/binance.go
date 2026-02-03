package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"

	"tradecore/internal/types"
)

// BinanceClient wraps the Binance API client
type BinanceClient struct {
	client *binance.Client
	logger *slog.Logger
}

// NewBinanceClient creates a new Binance API client
func NewBinanceClient(apiKey, secretKey string, logger *slog.Logger) *BinanceClient {
	client := binance.NewClient(apiKey, secretKey)
	return &BinanceClient{
		client: client,
		logger: logger,
	}
}

// ExecuteOrder places an order on Binance
func (b *BinanceClient) ExecuteOrder(ctx context.Context, req types.OrderRequest) (*types.OrderResult, error) {
	// Determine order side
	var side binance.SideType
	if req.Side == types.SideBuy {
		side = binance.SideTypeBuy
	} else {
		side = binance.SideTypeSell
	}

	// Format quantity with appropriate precision
	quantityStr := strconv.FormatFloat(req.Quantity, 'f', 8, 64)
	priceStr := strconv.FormatFloat(req.Price, 'f', 8, 64)

	// Create limit order
	order, err := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(side).
		Type(binance.OrderTypeLimit).
		TimeInForce(binance.TimeInForceTypeGTC).
		Quantity(quantityStr).
		Price(priceStr).
		Do(ctx)

	if err != nil {
		b.logger.Error("[BINANCE] Order failed",
			"symbol", req.Symbol,
			"side", req.Side,
			"error", err,
		)
		return &types.OrderResult{
			Success: false,
			Error:   err,
		}, nil
	}

	filledQty, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
	avgPrice := req.Price // Limit orders fill at limit price or better

	b.logger.Info("[BINANCE] Order placed",
		"order_id", order.OrderID,
		"symbol", req.Symbol,
		"side", req.Side,
		"status", order.Status,
	)

	return &types.OrderResult{
		Success:   true,
		OrderID:   fmt.Sprintf("%d", order.OrderID),
		FilledQty: filledQty,
		AvgPrice:  avgPrice,
	}, nil
}

// GetBalance returns the available balance for an asset
func (b *BinanceClient) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get account: %w", err)
	}

	for _, balance := range account.Balances {
		if balance.Asset == asset {
			free, _ := strconv.ParseFloat(balance.Free, 64)
			return free, nil
		}
	}

	return 0, nil
}

// PlaceOrder places an order on Binance using exchange types
func (b *BinanceClient) PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResult, error) {
	// Determine order side
	var side binance.SideType
	if req.Side == "BUY" {
		side = binance.SideTypeBuy
	} else {
		side = binance.SideTypeSell
	}

	// Determine order type
	var orderType binance.OrderType
	if req.Type == "LIMIT" {
		orderType = binance.OrderTypeLimit
	} else {
		orderType = binance.OrderTypeMarket
	}

	// Create order service
	service := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(side).
		Type(orderType)

	// Add quantity or quote order qty
	if req.Quantity != "" {
		service = service.Quantity(req.Quantity)
	}
	if req.QuoteOrderQty != "" {
		service = service.QuoteOrderQty(req.QuoteOrderQty)
	}
	if req.Price != "" && orderType == binance.OrderTypeLimit {
		service = service.Price(req.Price).TimeInForce(binance.TimeInForceTypeGTC)
	}

	order, err := service.Do(ctx)
	if err != nil {
		b.logger.Error("[BINANCE] PlaceOrder failed",
			"symbol", req.Symbol,
			"side", req.Side,
			"error", err,
		)
		return nil, err
	}

	// Convert fills
	fills := make([]OrderFill, len(order.Fills))
	for i, fill := range order.Fills {
		fills[i] = OrderFill{
			Price:           fill.Price,
			Qty:             fill.Quantity,
			Commission:      fill.Commission,
			CommissionAsset: fill.CommissionAsset,
		}
	}

	b.logger.Info("[BINANCE] Order placed",
		"order_id", order.OrderID,
		"symbol", req.Symbol,
		"side", req.Side,
		"status", order.Status,
	)

	return &OrderResult{
		OrderID:     fmt.Sprintf("%d", order.OrderID),
		Symbol:      order.Symbol,
		Status:      string(order.Status),
		ExecutedQty: order.ExecutedQuantity,
		Price:       order.Price,
		Fills:       fills,
	}, nil
}

// Close is a no-op for the REST client
func (b *BinanceClient) Close() error {
	return nil
}

// BinanceStreamer implements MarketStreamer for Binance WebSocket
type BinanceStreamer struct {
	logger        *slog.Logger
	mu            sync.RWMutex
	eventChan     chan types.Event
	subscriptions map[string]*wsSubscription
	ctx           context.Context
	cancel        context.CancelFunc
}

// wsSubscription holds the state for a single WebSocket connection
type wsSubscription struct {
	symbol   string
	stopChan chan struct{}
	done     chan struct{}
}

// NewBinanceStreamer creates a new Binance market data streamer
func NewBinanceStreamer(logger *slog.Logger) *BinanceStreamer {
	ctx, cancel := context.WithCancel(context.Background())
	return &BinanceStreamer{
		logger:        logger,
		eventChan:     make(chan types.Event, 1000),
		subscriptions: make(map[string]*wsSubscription),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Subscribe starts streaming price updates for a symbol
func (s *BinanceStreamer) Subscribe(ctx context.Context, symbol string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already subscribed
	if _, exists := s.subscriptions[symbol]; exists {
		s.logger.Debug("[BINANCE] Already subscribed", "symbol", symbol)
		return nil
	}

	// Create subscription
	sub := &wsSubscription{
		symbol:   symbol,
		stopChan: make(chan struct{}),
		done:     make(chan struct{}),
	}
	s.subscriptions[symbol] = sub

	// Start WebSocket connection with auto-reconnection
	go s.runWebSocket(sub)

	s.logger.Info("[BINANCE] Subscribed to symbol", "symbol", symbol)
	return nil
}

// runWebSocket manages the WebSocket connection with auto-reconnection
func (s *BinanceStreamer) runWebSocket(sub *wsSubscription) {
	defer close(sub.done)

	symbol := strings.ToLower(sub.symbol)
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-sub.stopChan:
			return
		case <-s.ctx.Done():
			return
		default:
		}

		// Create WebSocket handler
		handler := func(event *binance.WsAggTradeEvent) {
			price, err := strconv.ParseFloat(event.Price, 64)
			if err != nil {
				s.logger.Error("[BINANCE] Failed to parse price",
					"symbol", sub.symbol,
					"error", err,
				)
				return
			}

			qty, _ := strconv.ParseFloat(event.Quantity, 64)

			priceUpdate := types.Event{
				Type: types.EventTypePriceUpdate,
				PriceUpdate: &types.PriceUpdate{
					Symbol:    sub.symbol,
					Price:     price,
					Quantity:  qty,
					Timestamp: time.UnixMilli(event.Time),
				},
			}

			select {
			case s.eventChan <- priceUpdate:
			default:
				// Channel full, drop message to prevent blocking
				s.logger.Warn("[BINANCE] Event channel full, dropping update",
					"symbol", sub.symbol,
				)
			}
		}

		// Error handler
		errHandler := func(err error) {
			s.logger.Error("[BINANCE] WebSocket error",
				"symbol", sub.symbol,
				"error", err,
			)
		}

		// Start WebSocket connection
		doneC, stopC, err := binance.WsAggTradeServe(symbol, handler, errHandler)
		if err != nil {
			s.logger.Error("[BINANCE] Failed to connect WebSocket",
				"symbol", sub.symbol,
				"error", err,
				"retry_in", backoff,
			)

			// Wait before retry
			select {
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			case <-sub.stopChan:
				return
			case <-s.ctx.Done():
				return
			}
		}

		s.logger.Info("[BINANCE] WebSocket connected", "symbol", sub.symbol)
		backoff = time.Second // Reset backoff on successful connection

		// Wait for disconnect or stop signal
		select {
		case <-doneC:
			s.logger.Warn("[BINANCE] WebSocket disconnected, reconnecting...",
				"symbol", sub.symbol,
			)
		case <-sub.stopChan:
			close(stopC)
			return
		case <-s.ctx.Done():
			close(stopC)
			return
		}
	}
}

// Unsubscribe stops streaming price updates for a symbol
func (s *BinanceStreamer) Unsubscribe(symbol string) error {
	s.mu.Lock()
	sub, exists := s.subscriptions[symbol]
	if !exists {
		s.mu.Unlock()
		return nil
	}
	delete(s.subscriptions, symbol)
	s.mu.Unlock()

	// Stop the WebSocket goroutine
	close(sub.stopChan)

	// Wait for goroutine to finish
	select {
	case <-sub.done:
	case <-time.After(5 * time.Second):
		s.logger.Warn("[BINANCE] Timeout waiting for WebSocket to close",
			"symbol", symbol,
		)
	}

	s.logger.Info("[BINANCE] Unsubscribed from symbol", "symbol", symbol)
	return nil
}

// Events returns the channel for receiving price updates
func (s *BinanceStreamer) Events() <-chan types.Event {
	return s.eventChan
}

// Close stops all subscriptions and cleans up
func (s *BinanceStreamer) Close() error {
	s.cancel() // Cancel context to stop all goroutines

	s.mu.Lock()
	symbols := make([]string, 0, len(s.subscriptions))
	for symbol := range s.subscriptions {
		symbols = append(symbols, symbol)
	}
	s.mu.Unlock()

	// Unsubscribe from all symbols
	for _, symbol := range symbols {
		s.Unsubscribe(symbol)
	}

	close(s.eventChan)
	s.logger.Info("[BINANCE] Streamer closed")
	return nil
}

// ActiveSymbols returns currently subscribed symbols
func (s *BinanceStreamer) ActiveSymbols() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	symbols := make([]string, 0, len(s.subscriptions))
	for symbol := range s.subscriptions {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetPrice returns current price for a symbol via REST API
func (s *BinanceStreamer) GetPrice(ctx context.Context, symbol string) (float64, error) {
	client := binance.NewClient("", "") // Public endpoint, no auth needed

	prices, err := client.NewListPricesService().Symbol(symbol).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get price for %s: %w", symbol, err)
	}

	if len(prices) == 0 {
		return 0, fmt.Errorf("no price found for %s", symbol)
	}

	price, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %w", err)
	}

	return price, nil
}

// GetKlines returns historical kline/candlestick data
func (s *BinanceStreamer) GetKlines(ctx context.Context, symbol string, interval string, limit int) ([]Kline, error) {
	client := binance.NewClient("", "") // Public endpoint, no auth needed

	klines, err := client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get klines for %s: %w", symbol, err)
	}

	result := make([]Kline, len(klines))
	for i, k := range klines {
		open, _ := strconv.ParseFloat(k.Open, 64)
		high, _ := strconv.ParseFloat(k.High, 64)
		low, _ := strconv.ParseFloat(k.Low, 64)
		close, _ := strconv.ParseFloat(k.Close, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)

		result[i] = Kline{
			OpenTime:  k.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			CloseTime: k.CloseTime,
		}
	}

	return result, nil
}
