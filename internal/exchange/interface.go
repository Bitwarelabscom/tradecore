package exchange

import (
	"context"

	"tradecore/internal/types"
)

// Executor defines the interface for executing trades
type Executor interface {
	// ExecuteOrder places a market or limit order (legacy)
	ExecuteOrder(ctx context.Context, req types.OrderRequest) (*types.OrderResult, error)

	// PlaceOrder places an order using exchange types
	PlaceOrder(ctx context.Context, req OrderRequest) (*OrderResult, error)

	// GetBalance returns the available balance for an asset
	GetBalance(ctx context.Context, asset string) (float64, error)

	// Close cleans up resources
	Close() error
}

// MarketStreamer defines the interface for streaming market data
type MarketStreamer interface {
	// Subscribe starts streaming price updates for a symbol
	Subscribe(ctx context.Context, symbol string) error

	// Unsubscribe stops streaming price updates for a symbol
	Unsubscribe(symbol string) error

	// Events returns the channel for receiving price updates
	Events() <-chan types.Event

	// GetPrice returns current price for a symbol
	GetPrice(ctx context.Context, symbol string) (float64, error)

	// GetKlines returns historical kline/candlestick data
	GetKlines(ctx context.Context, symbol string, interval string, limit int) ([]Kline, error)

	// Close cleans up all connections
	Close() error
}

// Kline represents candlestick/OHLCV data
type Kline struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

// OrderRequest for placing orders
type OrderRequest struct {
	Symbol        string
	Side          string // BUY or SELL
	Type          string // MARKET or LIMIT
	Quantity      string
	QuoteOrderQty string
	Price         string
	TimeInForce   string
}

// OrderResult from order execution
type OrderResult struct {
	OrderID     string
	Symbol      string
	Status      string
	ExecutedQty string
	Price       string
	Fills       []OrderFill
}

// OrderFill represents a single fill
type OrderFill struct {
	Price           string
	Qty             string
	Commission      string
	CommissionAsset string
}

// SubscriptionManager handles dynamic WebSocket subscriptions
type SubscriptionManager interface {
	// AddSymbol subscribes to a new symbol's market data
	AddSymbol(symbol string) error

	// RemoveSymbol unsubscribes from a symbol's market data
	RemoveSymbol(symbol string) error

	// ActiveSymbols returns currently subscribed symbols
	ActiveSymbols() []string
}
