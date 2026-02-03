package types

import (
	"time"
)

// Action represents the type of signal action
type Action string

const (
	ActionStartGrid         Action = "START_GRID"
	ActionStopGrid          Action = "STOP_GRID"
	ActionStartTrailingStop Action = "START_TRAILING_STOP"
	ActionStopTrailingStop  Action = "STOP_TRAILING_STOP"
	ActionStartDCA          Action = "START_DCA"
	ActionStopDCA           Action = "STOP_DCA"
	ActionStartRSI          Action = "START_RSI"
	ActionStopRSI           Action = "STOP_RSI"
	ActionStartMACrossover  Action = "START_MA_CROSSOVER"
	ActionStopMACrossover   Action = "STOP_MA_CROSSOVER"
	ActionStartMACD         Action = "START_MACD"
	ActionStopMACD          Action = "STOP_MACD"
	ActionStartBreakout     Action = "START_BREAKOUT"
	ActionStopBreakout      Action = "STOP_BREAKOUT"
	ActionStartMeanReversion Action = "START_MEAN_REVERSION"
	ActionStopMeanReversion  Action = "STOP_MEAN_REVERSION"
	ActionStartMomentum      Action = "START_MOMENTUM"
	ActionStopMomentum       Action = "STOP_MOMENTUM"
)

// BotType represents the type of trading bot
type BotType string

const (
	BotTypeGrid          BotType = "grid"
	BotTypeDCA           BotType = "dca"
	BotTypeRSI           BotType = "rsi"
	BotTypeMACrossover   BotType = "ma_crossover"
	BotTypeMACD          BotType = "macd"
	BotTypeBreakout      BotType = "breakout"
	BotTypeMeanReversion BotType = "mean_reversion"
	BotTypeMomentum      BotType = "momentum"
	BotTypeCustom        BotType = "custom"
)

// Side represents buy or sell
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// GridConfig holds configuration for a grid bot
type GridConfig struct {
	UpperPrice float64 `json:"upper_price"`
	LowerPrice float64 `json:"lower_price"`
	GridLines  int     `json:"grid_lines"`
	Quantity   float64 `json:"quantity"` // Quantity per grid level
}

// TrailingStopConfig holds configuration for trailing stop loss
type TrailingStopConfig struct {
	ActivationPrice float64 `json:"activation_price"` // Optional: price at which trailing activates (0 = immediate)
	CallbackRate    float64 `json:"callback_rate"`    // Percentage as decimal (e.g., 0.02 for 2%)
	Quantity        float64 `json:"quantity"`         // Quantity to sell when triggered
}

// DCAConfig holds configuration for a DCA bot
type DCAConfig struct {
	Amount          float64 `json:"amount"`            // Amount to invest per interval (in quote currency)
	Interval        string  `json:"interval"`          // "1h", "4h", "1d", "1w"
	MaxOrders       int     `json:"max_orders"`        // Maximum number of orders (0 = unlimited)
	PriceDeviation  float64 `json:"price_deviation"`   // Optional: only buy if price drops X% from last buy
	TakeProfit      float64 `json:"take_profit"`       // Optional: take profit percentage
	StopLoss        float64 `json:"stop_loss"`         // Optional: stop loss percentage
}

// RSIConfig holds configuration for an RSI bot
type RSIConfig struct {
	Period         int     `json:"period"`           // RSI period (default 14)
	OversoldLevel  float64 `json:"oversold_level"`   // Buy below this level (default 30)
	OverboughtLevel float64 `json:"overbought_level"` // Sell above this level (default 70)
	Quantity       float64 `json:"quantity"`         // Quantity per trade
	Timeframe      string  `json:"timeframe"`        // "1m", "5m", "15m", "1h", "4h"
	TakeProfit     float64 `json:"take_profit"`      // Optional: take profit percentage
	StopLoss       float64 `json:"stop_loss"`        // Optional: stop loss percentage
}

// MACrossoverConfig holds configuration for a MA crossover bot
type MACrossoverConfig struct {
	FastPeriod   int     `json:"fast_period"`   // Fast MA period (default 9)
	SlowPeriod   int     `json:"slow_period"`   // Slow MA period (default 21)
	MAType       string  `json:"ma_type"`       // "SMA" or "EMA"
	Quantity     float64 `json:"quantity"`      // Quantity per trade
	Timeframe    string  `json:"timeframe"`     // "1m", "5m", "15m", "1h", "4h"
	TakeProfit   float64 `json:"take_profit"`   // Optional: take profit percentage
	StopLoss     float64 `json:"stop_loss"`     // Optional: stop loss percentage
}

// MACDConfig holds configuration for a MACD bot
type MACDConfig struct {
	FastPeriod   int     `json:"fast_period"`   // Fast EMA period (default 12)
	SlowPeriod   int     `json:"slow_period"`   // Slow EMA period (default 26)
	SignalPeriod int     `json:"signal_period"` // Signal line period (default 9)
	Quantity     float64 `json:"quantity"`      // Quantity per trade
	Timeframe    string  `json:"timeframe"`     // "1m", "5m", "15m", "1h", "4h"
	TakeProfit   float64 `json:"take_profit"`   // Optional: take profit percentage
	StopLoss     float64 `json:"stop_loss"`     // Optional: stop loss percentage
}

// BreakoutConfig holds configuration for a breakout bot
type BreakoutConfig struct {
	LookbackPeriod    int     `json:"lookback_period"`     // Periods to look back for high/low (default 20)
	BreakoutThreshold float64 `json:"breakout_threshold"`  // Percentage above/below to confirm breakout
	VolumeMultiplier  float64 `json:"volume_multiplier"`   // Required volume increase (default 1.5x)
	Quantity          float64 `json:"quantity"`            // Quantity per trade
	Timeframe         string  `json:"timeframe"`           // "1m", "5m", "15m", "1h", "4h"
	TakeProfit        float64 `json:"take_profit"`         // Optional: take profit percentage
	StopLoss          float64 `json:"stop_loss"`           // Optional: stop loss percentage
}

// MeanReversionConfig holds configuration for a mean reversion bot
type MeanReversionConfig struct {
	MAPeriod           int     `json:"ma_period"`            // MA period for mean calculation (default 20)
	MAType             string  `json:"ma_type"`              // "SMA" or "EMA"
	DeviationThreshold float64 `json:"deviation_threshold"`  // Percentage deviation to trigger (default 2%)
	Quantity           float64 `json:"quantity"`             // Quantity per trade
	Timeframe          string  `json:"timeframe"`            // "1m", "5m", "15m", "1h", "4h"
	TakeProfit         float64 `json:"take_profit"`          // Optional: take profit percentage
	StopLoss           float64 `json:"stop_loss"`            // Optional: stop loss percentage
}

// MomentumConfig holds configuration for a momentum bot
type MomentumConfig struct {
	RSIPeriod       int     `json:"rsi_period"`        // RSI period (default 14)
	RSIThreshold    float64 `json:"rsi_threshold"`     // RSI threshold for momentum (default 50)
	VolumeMultiplier float64 `json:"volume_multiplier"` // Required volume increase (default 1.2x)
	Quantity        float64 `json:"quantity"`          // Quantity per trade
	Timeframe       string  `json:"timeframe"`         // "1m", "5m", "15m", "1h", "4h"
	TakeProfit      float64 `json:"take_profit"`       // Optional: take profit percentage
	StopLoss        float64 `json:"stop_loss"`         // Optional: stop loss percentage
}

// SignalEvent represents an incoming trading signal (legacy)
type SignalEvent struct {
	Symbol     string              `json:"symbol"`
	Action     Action              `json:"action"`
	GridConfig *GridConfig         `json:"grid_config,omitempty"`
	SLConfig   *TrailingStopConfig `json:"sl_config,omitempty"`
	Timestamp  time.Time           `json:"timestamp"`
}

// PriceUpdate represents a real-time price update from market data
type PriceUpdate struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Volume24h float64   `json:"volume_24h"`
	Timestamp time.Time `json:"timestamp"`
}

// Kline represents OHLCV candlestick data
type Kline struct {
	OpenTime  time.Time `json:"open_time"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	CloseTime time.Time `json:"close_time"`
}

// GridLevel represents a single level in the grid
type GridLevel struct {
	Price       float64 `json:"price"`
	IsBuyFilled bool    `json:"is_buy_filled"`
	Quantity    float64 `json:"quantity"`
}

// GridState holds the current state of a grid bot
type GridState struct {
	Active     bool        `json:"active"`
	UpperPrice float64     `json:"upper_price"`
	LowerPrice float64     `json:"lower_price"`
	GridStep   float64     `json:"grid_step"`
	Levels     []GridLevel `json:"levels"`
	LastPrice  float64     `json:"last_price"`
}

// TrailingStopState holds the current state of a trailing stop
type TrailingStopState struct {
	Active          bool    `json:"active"`
	ActivationPrice float64 `json:"activation_price"`
	CallbackRate    float64 `json:"callback_rate"`
	Quantity        float64 `json:"quantity"`
	HighestPrice    float64 `json:"highest_price"`
	Activated       bool    `json:"activated"` // Whether activation price was reached
}

// DCAState holds the current state of a DCA bot
type DCAState struct {
	Active          bool      `json:"active"`
	TotalInvested   float64   `json:"total_invested"`
	TotalQuantity   float64   `json:"total_quantity"`
	AveragePrice    float64   `json:"average_price"`
	OrderCount      int       `json:"order_count"`
	LastOrderTime   time.Time `json:"last_order_time"`
	LastOrderPrice  float64   `json:"last_order_price"`
	NextOrderTime   time.Time `json:"next_order_time"`
	Config          DCAConfig `json:"config"`
}

// RSIState holds the current state of an RSI bot
type RSIState struct {
	Active        bool      `json:"active"`
	LastRSI       float64   `json:"last_rsi"`
	Position      string    `json:"position"`       // "none", "long"
	EntryPrice    float64   `json:"entry_price"`
	EntryQuantity float64   `json:"entry_quantity"`
	LastSignal    string    `json:"last_signal"`    // "buy", "sell", "none"
	LastSignalTime time.Time `json:"last_signal_time"`
	Config        RSIConfig `json:"config"`
}

// MACrossoverState holds the current state of a MA crossover bot
type MACrossoverState struct {
	Active        bool              `json:"active"`
	LastFastMA    float64           `json:"last_fast_ma"`
	LastSlowMA    float64           `json:"last_slow_ma"`
	Position      string            `json:"position"`        // "none", "long"
	EntryPrice    float64           `json:"entry_price"`
	EntryQuantity float64           `json:"entry_quantity"`
	LastCrossover string            `json:"last_crossover"`  // "golden", "death", "none"
	LastSignalTime time.Time        `json:"last_signal_time"`
	Config        MACrossoverConfig `json:"config"`
}

// MACDState holds the current state of a MACD bot
type MACDState struct {
	Active         bool       `json:"active"`
	LastMACD       float64    `json:"last_macd"`
	LastSignal     float64    `json:"last_signal"`
	LastHistogram  float64    `json:"last_histogram"`
	Position       string     `json:"position"`        // "none", "long"
	EntryPrice     float64    `json:"entry_price"`
	EntryQuantity  float64    `json:"entry_quantity"`
	LastCrossover  string     `json:"last_crossover"`  // "bullish", "bearish", "none"
	LastSignalTime time.Time  `json:"last_signal_time"`
	Config         MACDConfig `json:"config"`
}

// BreakoutState holds the current state of a breakout bot
type BreakoutState struct {
	Active         bool           `json:"active"`
	HighestHigh    float64        `json:"highest_high"`
	LowestLow      float64        `json:"lowest_low"`
	Position       string         `json:"position"`        // "none", "long", "short"
	EntryPrice     float64        `json:"entry_price"`
	EntryQuantity  float64        `json:"entry_quantity"`
	BreakoutType   string         `json:"breakout_type"`   // "high", "low", "none"
	LastSignalTime time.Time      `json:"last_signal_time"`
	Config         BreakoutConfig `json:"config"`
}

// MeanReversionState holds the current state of a mean reversion bot
type MeanReversionState struct {
	Active         bool                `json:"active"`
	LastMA         float64             `json:"last_ma"`
	LastDeviation  float64             `json:"last_deviation"`
	Position       string              `json:"position"`        // "none", "long"
	EntryPrice     float64             `json:"entry_price"`
	EntryQuantity  float64             `json:"entry_quantity"`
	LastSignalTime time.Time           `json:"last_signal_time"`
	Config         MeanReversionConfig `json:"config"`
}

// MomentumState holds the current state of a momentum bot
type MomentumState struct {
	Active         bool           `json:"active"`
	LastRSI        float64        `json:"last_rsi"`
	LastVolume     float64        `json:"last_volume"`
	AvgVolume      float64        `json:"avg_volume"`
	Position       string         `json:"position"`        // "none", "long"
	EntryPrice     float64        `json:"entry_price"`
	EntryQuantity  float64        `json:"entry_quantity"`
	LastSignalTime time.Time      `json:"last_signal_time"`
	Config         MomentumConfig `json:"config"`
}

// BotState holds all state for a single bot (keyed by bot ID)
type BotState struct {
	BotID          string              `json:"bot_id"`
	UserID         string              `json:"user_id"`
	Symbol         string              `json:"symbol"`
	BotType        string              `json:"bot_type"`
	Grid           *GridState          `json:"grid,omitempty"`
	TrailingStop   *TrailingStopState  `json:"trailing_stop,omitempty"`
	DCA            *DCAState           `json:"dca,omitempty"`
	RSI            *RSIState           `json:"rsi,omitempty"`
	MACrossover    *MACrossoverState   `json:"ma_crossover,omitempty"`
	MACD           *MACDState          `json:"macd,omitempty"`
	Breakout       *BreakoutState      `json:"breakout,omitempty"`
	MeanReversion  *MeanReversionState `json:"mean_reversion,omitempty"`
	Momentum       *MomentumState      `json:"momentum,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// TradecoreStateJSON is the JSON structure stored in trading_bots.tradecore_state
type TradecoreStateJSON struct {
	Grid           *GridState          `json:"grid,omitempty"`
	TrailingStop   *TrailingStopState  `json:"trailing_stop,omitempty"`
	DCA            *DCAState           `json:"dca,omitempty"`
	RSI            *RSIState           `json:"rsi,omitempty"`
	MACrossover    *MACrossoverState   `json:"ma_crossover,omitempty"`
	MACD           *MACDState          `json:"macd,omitempty"`
	Breakout       *BreakoutState      `json:"breakout,omitempty"`
	MeanReversion  *MeanReversionState `json:"mean_reversion,omitempty"`
	Momentum       *MomentumState      `json:"momentum,omitempty"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// TradeRecord represents a trade to be recorded in the database
type TradeRecord struct {
	UserID          string     `json:"user_id"`
	BotID           string     `json:"bot_id"`
	Symbol          string     `json:"symbol"`
	Side            string     `json:"side"`
	OrderType       string     `json:"order_type"`
	Quantity        float64    `json:"quantity"`
	Price           float64    `json:"price"`
	FilledPrice     float64    `json:"filled_price"`
	Total           float64    `json:"total"`
	Fee             float64    `json:"fee"`
	FeeAsset        string     `json:"fee_asset"`
	Status          string     `json:"status"`
	BinanceOrderID  string     `json:"binance_order_id"`
	TradecoreOrderID string    `json:"tradecore_order_id"`
	FilledAt        *time.Time `json:"filled_at"`
}

// TradeSignal represents a signal from a strategy to execute a trade
type TradeSignal struct {
	Side     Side
	Quantity float64
	Price    float64 // 0 for market order
	Reason   string
}

// EventType distinguishes between different event types in the fan-in channel
type EventType int

const (
	EventTypeSignal EventType = iota
	EventTypePriceUpdate
	EventTypeSubscribe
	EventTypeUnsubscribe
	EventTypeBotCommand
	EventTypeKlineUpdate
)

// BotCommand represents a command to start/stop a bot
type BotCommand struct {
	Command   string          `json:"command"` // "start", "stop", "update"
	BotID     string          `json:"bot_id"`
	UserID    string          `json:"user_id"`
	BotType   string          `json:"bot_type"`
	Symbol    string          `json:"symbol"`
	Config    interface{}     `json:"config"`
}

// Event is the unified event type for the fan-in channel
type Event struct {
	Type        EventType
	Signal      *SignalEvent
	PriceUpdate *PriceUpdate
	BotCommand  *BotCommand
	Klines      []Kline
	Symbol      string // Used for subscribe/unsubscribe events
}

// StateSnapshot represents the complete state for persistence (legacy)
type StateSnapshot struct {
	ActiveBots map[string]*BotState `json:"active_bots"`
	SavedAt    time.Time            `json:"saved_at"`
	Version    int                  `json:"version"`
}

// OrderRequest represents a trade order to be executed
type OrderRequest struct {
	Symbol   string
	Side     Side
	Quantity float64
	Price    float64
	UserID   string
	BotID    string
}

// OrderResult represents the result of an order execution
type OrderResult struct {
	Success   bool
	OrderID   string
	FilledQty float64
	AvgPrice  float64
	Fee       float64
	FeeAsset  string
	Error     error
}

// StartBotRequest represents a request to start a bot via HTTP API
type StartBotRequest struct {
	UserID  string      `json:"user_id"`
	BotID   string      `json:"bot_id"`
	Type    string      `json:"type"`
	Symbol  string      `json:"symbol"`
	Config  interface{} `json:"config"`
}

// StopBotRequest represents a request to stop a bot via HTTP API
type StopBotRequest struct {
	BotID string `json:"bot_id"`
}

// BotStatusResponse represents the status of a bot
type BotStatusResponse struct {
	BotID       string      `json:"bot_id"`
	UserID      string      `json:"user_id"`
	Status      string      `json:"status"`
	Symbol      string      `json:"symbol"`
	Type        string      `json:"type"`
	TotalProfit float64     `json:"total_profit"`
	TotalTrades int         `json:"total_trades"`
	LastError   string      `json:"last_error,omitempty"`
	State       interface{} `json:"state,omitempty"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
	ActiveBots int      `json:"active_bots"`
}
