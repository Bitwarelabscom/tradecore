package conditional

import (
	"time"
)

// ConditionType represents the type of trigger condition
type ConditionType string

const (
	ConditionTypePrice  ConditionType = "price"
	ConditionTypeRSI    ConditionType = "rsi"
	ConditionTypeMACD   ConditionType = "macd"
	ConditionTypeEMA    ConditionType = "ema"
	ConditionTypeSMA    ConditionType = "sma"
	ConditionTypeVolume ConditionType = "volume"
)

// Operator represents comparison operators
type Operator string

const (
	OpLessThan      Operator = "<"
	OpLessEqual     Operator = "<="
	OpGreaterThan   Operator = ">"
	OpGreaterEqual  Operator = ">="
	OpCrossesAbove  Operator = "crosses_above"
	OpCrossesBelow  Operator = "crosses_below"
)

// Logic for combining conditions
type Logic string

const (
	LogicAND Logic = "AND"
	LogicOR  Logic = "OR"
)

// OrderStatus represents the status of a conditional order
type OrderStatus string

const (
	StatusActive          OrderStatus = "active"
	StatusPending         OrderStatus = "pending"
	StatusMonitoring      OrderStatus = "monitoring"
	StatusTriggered       OrderStatus = "triggered"
	StatusExecuting       OrderStatus = "executing"
	StatusExecuted        OrderStatus = "executed"
	StatusPartiallyFilled OrderStatus = "partially_filled"
	StatusCancelled       OrderStatus = "cancelled"
	StatusFailed          OrderStatus = "failed"
	StatusExpired         OrderStatus = "expired"
)

// Condition represents a single trigger condition
type Condition struct {
	Type     ConditionType `json:"type"`
	Operator Operator      `json:"operator"`
	Value    float64       `json:"value"`

	// For indicators that need periods
	Period int `json:"period,omitempty"`

	// For MACD
	FastPeriod   int `json:"fast_period,omitempty"`
	SlowPeriod   int `json:"slow_period,omitempty"`
	SignalPeriod int `json:"signal_period,omitempty"`

	// For EMA/SMA comparisons
	CompareTo string `json:"compare_to,omitempty"` // "price", "ema_X", "sma_X"

	// Runtime state for crosses_above/crosses_below
	PreviousValue *float64 `json:"-"`
}

// TriggerConditions represents compound trigger conditions
type TriggerConditions struct {
	Logic      Logic       `json:"logic"`
	Conditions []Condition `json:"conditions"`
}

// OrderAction represents the action to take when triggered
type OrderAction struct {
	Side        string   `json:"side"`         // "buy" or "sell"
	Type        string   `json:"type"`         // "market" or "limit"
	Quantity    *float64 `json:"quantity"`     // Exact quantity (optional)
	QuoteAmount *float64 `json:"quote_amount"` // Amount in quote currency (e.g., 15 USDC)
	Price       *float64 `json:"price"`        // For limit orders
}

// TrailingStopConfig for follow-up trailing stop
type TrailingStopConfig struct {
	ActivationPrice float64 `json:"activation_price"` // Price at which to start trailing
	InitialSL       float64 `json:"initial_sl"`       // Initial stop loss price
	CallbackPct     float64 `json:"callback_pct"`     // Trailing percentage (e.g., 2.0 for 2%)
}

// FollowUp represents post-execution actions
type FollowUp struct {
	TrailingStop *TrailingStopConfig `json:"trailing_stop,omitempty"`
	StopLoss     *float64            `json:"stop_loss,omitempty"`
	TakeProfit   *float64            `json:"take_profit,omitempty"`
}

// TriggerDetails captures the state when conditions were met
type TriggerDetails struct {
	Price      float64            `json:"price"`
	RSI        *float64           `json:"rsi,omitempty"`
	MACD       *float64           `json:"macd,omitempty"`
	MACDSignal *float64           `json:"macd_signal,omitempty"`
	EMA        map[int]float64    `json:"ema,omitempty"`
	SMA        map[int]float64    `json:"sma,omitempty"`
	Conditions map[string]float64 `json:"conditions"` // Each condition's current value
}

// ConditionalOrder represents a complete conditional order
type ConditionalOrder struct {
	ID                string            `json:"id"`
	UserID            string            `json:"user_id"`
	Symbol            string            `json:"symbol"`
	MarketType        string            `json:"market_type"`
	TriggerConditions TriggerConditions `json:"trigger_conditions"`
	Action            OrderAction       `json:"action"`
	FollowUp          *FollowUp         `json:"follow_up,omitempty"`
	Status            OrderStatus       `json:"status"`

	// Legacy fields (for backward compatibility)
	Condition    string   `json:"condition,omitempty"`     // "above", "below", etc.
	TriggerPrice *float64 `json:"trigger_price,omitempty"` // Simple price trigger

	// Execution tracking
	TriggeredAt    *time.Time      `json:"triggered_at,omitempty"`
	ExecutedAt     *time.Time      `json:"executed_at,omitempty"`
	TriggerDetails *TriggerDetails `json:"trigger_details,omitempty"`
	ResultTradeID  *string         `json:"result_trade_id,omitempty"`
	ResultOrderID  *string         `json:"result_order_id,omitempty"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
	RetryCount     int             `json:"retry_count"`

	// Metadata
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	Notes        *string    `json:"notes,omitempty"`
	Source       string     `json:"source"`
	OriginalText *string    `json:"original_text,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// CreateConditionalOrderRequest for API
type CreateConditionalOrderRequest struct {
	Symbol            string            `json:"symbol"`
	TriggerConditions TriggerConditions `json:"trigger_conditions"`
	Action            OrderAction       `json:"action"`
	FollowUp          *FollowUp         `json:"follow_up,omitempty"`
	ExpiresAt         *time.Time        `json:"expires_at,omitempty"`
	Notes             *string           `json:"notes,omitempty"`
	OriginalText      *string           `json:"original_text,omitempty"`
}

// ConditionCheckResult for monitoring
type ConditionCheckResult struct {
	OrderID     string
	IsMet       bool
	Details     TriggerDetails
	Error       error
}
