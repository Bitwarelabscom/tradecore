package conditional

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradecore/internal/exchange"
	"tradecore/internal/indicators"
)

// Monitor watches conditional orders and triggers them when conditions are met
type Monitor struct {
	pool           *pgxpool.Pool
	logger         *slog.Logger
	streamer       exchange.MarketStreamer
	executorGetter func(userID string) (exchange.Executor, error)
	mockMode       bool

	// Active orders by symbol for efficient checking
	ordersBySymbol map[string][]*ConditionalOrder
	mu             sync.RWMutex

	// Kline cache for indicator calculations
	klineCache map[string][]indicators.Kline
	klineMu    sync.RWMutex

	// Stop channel
	stopCh chan struct{}
}

// NewMonitor creates a new conditional order monitor
func NewMonitor(
	pool *pgxpool.Pool,
	streamer exchange.MarketStreamer,
	executorGetter func(userID string) (exchange.Executor, error),
	logger *slog.Logger,
	mockMode bool,
) *Monitor {
	return &Monitor{
		pool:           pool,
		logger:         logger,
		streamer:       streamer,
		executorGetter: executorGetter,
		mockMode:       mockMode,
		ordersBySymbol: make(map[string][]*ConditionalOrder),
		klineCache:     make(map[string][]indicators.Kline),
		stopCh:         make(chan struct{}),
	}
}

// Start begins monitoring conditional orders
func (m *Monitor) Start(ctx context.Context) error {
	m.logger.Info("[CONDITIONAL] Starting conditional order monitor")

	// Load active orders
	if err := m.loadActiveOrders(ctx); err != nil {
		return fmt.Errorf("failed to load active orders: %w", err)
	}

	// Start monitoring loop
	go m.monitorLoop(ctx)

	// Start kline fetcher for indicators
	go m.klineFetchLoop(ctx)

	return nil
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	close(m.stopCh)
	m.logger.Info("[CONDITIONAL] Conditional order monitor stopped")
}

// loadActiveOrders loads all active conditional orders from database
func (m *Monitor) loadActiveOrders(ctx context.Context) error {
	rows, err := m.pool.Query(ctx, `
		SELECT id, user_id, symbol, COALESCE(market_type, 'spot'),
		       condition, trigger_price, trigger_conditions, action, follow_up,
		       status, expires_at, notes, COALESCE(source, 'manual'), original_text,
		       created_at, COALESCE(updated_at, created_at)
		FROM conditional_orders
		WHERE status IN ('active', 'pending', 'monitoring')
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.ordersBySymbol = make(map[string][]*ConditionalOrder)

	for rows.Next() {
		order := &ConditionalOrder{}
		var triggerConditionsJSON, actionJSON, followUpJSON []byte
		var triggerPrice *float64
		var condition *string
		var expiresAt, notes, originalText *string

		err := rows.Scan(
			&order.ID, &order.UserID, &order.Symbol, &order.MarketType,
			&condition, &triggerPrice, &triggerConditionsJSON, &actionJSON, &followUpJSON,
			&order.Status, &expiresAt, &notes, &order.Source, &originalText,
			&order.CreatedAt, &order.UpdatedAt,
		)
		if err != nil {
			m.logger.Error("[CONDITIONAL] Failed to scan order", "error", err)
			continue
		}

		// Parse legacy condition
		if condition != nil {
			order.Condition = *condition
		}
		order.TriggerPrice = triggerPrice

		// Parse JSON fields
		if len(triggerConditionsJSON) > 0 {
			json.Unmarshal(triggerConditionsJSON, &order.TriggerConditions)
		}
		if len(actionJSON) > 0 {
			json.Unmarshal(actionJSON, &order.Action)
		}
		if len(followUpJSON) > 0 {
			json.Unmarshal(followUpJSON, &order.FollowUp)
		}

		if notes != nil {
			order.Notes = notes
		}
		if originalText != nil {
			order.OriginalText = originalText
		}

		// Add to symbol map
		m.ordersBySymbol[order.Symbol] = append(m.ordersBySymbol[order.Symbol], order)
	}

	totalOrders := 0
	for _, orders := range m.ordersBySymbol {
		totalOrders += len(orders)
	}

	m.logger.Info("[CONDITIONAL] Loaded active orders",
		"total", totalOrders,
		"symbols", len(m.ordersBySymbol),
	)

	return nil
}

// monitorLoop periodically checks conditions
func (m *Monitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkAllOrders(ctx)
		}
	}
}

// klineFetchLoop fetches klines for indicator calculations
func (m *Monitor) klineFetchLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Fetch every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.fetchKlines(ctx)
		}
	}
}

// fetchKlines fetches klines for all monitored symbols
func (m *Monitor) fetchKlines(ctx context.Context) {
	m.mu.RLock()
	symbols := make([]string, 0, len(m.ordersBySymbol))
	for symbol := range m.ordersBySymbol {
		symbols = append(symbols, symbol)
	}
	m.mu.RUnlock()

	for _, symbol := range symbols {
		klines, err := m.streamer.GetKlines(ctx, symbol, "15m", 100)
		if err != nil {
			m.logger.Debug("[CONDITIONAL] Failed to fetch klines", "symbol", symbol, "error", err)
			continue
		}

		// Convert to indicator klines
		indicatorKlines := make([]indicators.Kline, len(klines))
		for i, k := range klines {
			indicatorKlines[i] = indicators.Kline{
				OpenTime:  k.OpenTime,
				Open:      k.Open,
				High:      k.High,
				Low:       k.Low,
				Close:     k.Close,
				Volume:    k.Volume,
				CloseTime: k.CloseTime,
			}
		}

		m.klineMu.Lock()
		m.klineCache[symbol] = indicatorKlines
		m.klineMu.Unlock()
	}
}

// checkAllOrders checks all active orders
func (m *Monitor) checkAllOrders(ctx context.Context) {
	m.mu.RLock()
	symbolsCopy := make(map[string][]*ConditionalOrder)
	for symbol, orders := range m.ordersBySymbol {
		ordersCopy := make([]*ConditionalOrder, len(orders))
		copy(ordersCopy, orders)
		symbolsCopy[symbol] = ordersCopy
	}
	m.mu.RUnlock()

	for symbol, orders := range symbolsCopy {
		// Get current price
		price, err := m.streamer.GetPrice(ctx, symbol)
		if err != nil {
			m.logger.Debug("[CONDITIONAL] Failed to get price", "symbol", symbol, "error", err)
			continue
		}

		// Get klines for indicators
		m.klineMu.RLock()
		klines := m.klineCache[symbol]
		m.klineMu.RUnlock()

		for _, order := range orders {
			// Check expiration
			if order.ExpiresAt != nil && time.Now().After(*order.ExpiresAt) {
				m.expireOrder(ctx, order)
				continue
			}

			// Check conditions
			met, details := m.checkConditions(order, price, klines)
			if met {
				m.triggerOrder(ctx, order, details)
			}
		}
	}
}

// checkConditions checks if order conditions are met
func (m *Monitor) checkConditions(order *ConditionalOrder, price float64, klines []indicators.Kline) (bool, TriggerDetails) {
	details := TriggerDetails{
		Price:      price,
		Conditions: make(map[string]float64),
	}

	// Handle legacy simple conditions
	if order.TriggerPrice != nil && order.Condition != "" {
		return m.checkLegacyCondition(order, price, details)
	}

	// Handle compound conditions
	if len(order.TriggerConditions.Conditions) == 0 {
		return false, details
	}

	// Get closes for indicators
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}
	if len(closes) > 0 {
		closes = append(closes, price) // Add current price
	}

	results := make([]bool, len(order.TriggerConditions.Conditions))

	for i, cond := range order.TriggerConditions.Conditions {
		var currentValue float64

		switch cond.Type {
		case ConditionTypePrice:
			currentValue = price
			details.Conditions["price"] = price

		case ConditionTypeRSI:
			period := cond.Period
			if period == 0 {
				period = 14
			}
			if len(closes) >= period+1 {
				currentValue = indicators.RSI(closes, period)
				details.RSI = &currentValue
				details.Conditions[fmt.Sprintf("rsi_%d", period)] = currentValue
			}

		case ConditionTypeMACD:
			fast := cond.FastPeriod
			if fast == 0 {
				fast = 12
			}
			slow := cond.SlowPeriod
			if slow == 0 {
				slow = 26
			}
			signal := cond.SignalPeriod
			if signal == 0 {
				signal = 9
			}
			if len(closes) >= slow {
				macd, signalLine, _ := indicators.MACD(closes, fast, slow, signal)
				currentValue = macd
				details.MACD = &macd
				details.MACDSignal = &signalLine
				details.Conditions["macd"] = macd
			}

		case ConditionTypeEMA:
			period := cond.Period
			if period == 0 {
				period = 20
			}
			if len(closes) >= period {
				currentValue = indicators.EMA(closes, period)
				if details.EMA == nil {
					details.EMA = make(map[int]float64)
				}
				details.EMA[period] = currentValue
				details.Conditions[fmt.Sprintf("ema_%d", period)] = currentValue
			}

		case ConditionTypeSMA:
			period := cond.Period
			if period == 0 {
				period = 20
			}
			if len(closes) >= period {
				currentValue = indicators.SMA(closes, period)
				if details.SMA == nil {
					details.SMA = make(map[int]float64)
				}
				details.SMA[period] = currentValue
				details.Conditions[fmt.Sprintf("sma_%d", period)] = currentValue
			}

		case ConditionTypeVolume:
			if len(klines) > 0 {
				currentValue = klines[len(klines)-1].Volume
				details.Conditions["volume"] = currentValue
			}
		}

		// Compare value
		results[i] = m.compareValue(currentValue, cond.Operator, cond.Value, cond.PreviousValue)

		// Update previous value for cross detection
		order.TriggerConditions.Conditions[i].PreviousValue = &currentValue
	}

	// Combine results based on logic
	var finalResult bool
	if order.TriggerConditions.Logic == LogicOR {
		finalResult = false
		for _, r := range results {
			if r {
				finalResult = true
				break
			}
		}
	} else { // AND
		finalResult = true
		for _, r := range results {
			if !r {
				finalResult = false
				break
			}
		}
	}

	return finalResult, details
}

// checkLegacyCondition checks simple price conditions
func (m *Monitor) checkLegacyCondition(order *ConditionalOrder, price float64, details TriggerDetails) (bool, TriggerDetails) {
	triggerPrice := *order.TriggerPrice

	switch order.Condition {
	case "above":
		return price >= triggerPrice, details
	case "below":
		return price <= triggerPrice, details
	case "crosses_up":
		// Would need previous price tracking
		return price >= triggerPrice, details
	case "crosses_down":
		return price <= triggerPrice, details
	}

	return false, details
}

// compareValue compares current value against target
func (m *Monitor) compareValue(current float64, op Operator, target float64, previous *float64) bool {
	switch op {
	case OpLessThan:
		return current < target
	case OpLessEqual:
		return current <= target
	case OpGreaterThan:
		return current > target
	case OpGreaterEqual:
		return current >= target
	case OpCrossesAbove:
		if previous == nil {
			return false
		}
		return *previous < target && current >= target
	case OpCrossesBelow:
		if previous == nil {
			return false
		}
		return *previous > target && current <= target
	}
	return false
}

// triggerOrder triggers an order for execution
func (m *Monitor) triggerOrder(ctx context.Context, order *ConditionalOrder, details TriggerDetails) {
	m.logger.Info("[CONDITIONAL] Order triggered",
		"order_id", order.ID,
		"symbol", order.Symbol,
		"price", details.Price,
	)

	// Update status to triggered
	now := time.Now()
	detailsJSON, _ := json.Marshal(details)

	_, err := m.pool.Exec(ctx, `
		UPDATE conditional_orders
		SET status = 'triggered', triggered_at = $2, trigger_details = $3
		WHERE id = $1
	`, order.ID, now, detailsJSON)

	if err != nil {
		m.logger.Error("[CONDITIONAL] Failed to update triggered status", "error", err)
		return
	}

	// Log event
	m.logEvent(ctx, order.ID, "triggered", map[string]interface{}{
		"price":   details.Price,
		"details": details,
	})

	// Execute the order
	go m.executeOrder(ctx, order, details)

	// Remove from active monitoring
	m.removeOrder(order)
}

// executeOrder executes the triggered order
func (m *Monitor) executeOrder(ctx context.Context, order *ConditionalOrder, details TriggerDetails) {
	m.logger.Info("[CONDITIONAL] Executing order",
		"order_id", order.ID,
		"symbol", order.Symbol,
		"action", order.Action,
	)

	// Update status to executing
	m.pool.Exec(ctx, `UPDATE conditional_orders SET status = 'executing' WHERE id = $1`, order.ID)

	// Get executor for user
	executor, err := m.executorGetter(order.UserID)
	if err != nil {
		m.failOrder(ctx, order, fmt.Sprintf("Failed to get executor: %v", err))
		return
	}

	if executor == nil && !m.mockMode {
		m.failOrder(ctx, order, "No executor available for user")
		return
	}

	// Prepare order
	side := "BUY"
	if order.Action.Side == "sell" {
		side = "SELL"
	}

	orderType := "MARKET"
	if order.Action.Type == "limit" {
		orderType = "LIMIT"
	}

	var quantity string
	if order.Action.Quantity != nil {
		quantity = fmt.Sprintf("%f", *order.Action.Quantity)
	}

	var quoteQty string
	if order.Action.QuoteAmount != nil {
		quoteQty = fmt.Sprintf("%f", *order.Action.QuoteAmount)
	}

	// Execute
	var binanceOrder *exchange.OrderResult
	if m.mockMode {
		// Mock execution
		binanceOrder = &exchange.OrderResult{
			OrderID:     fmt.Sprintf("MOCK_%s", order.ID),
			Symbol:      order.Symbol,
			Status:      "FILLED",
			ExecutedQty: quantity,
		}
		m.logger.Info("[CONDITIONAL] Mock order executed", "order_id", order.ID)
	} else {
		binanceOrder, err = executor.PlaceOrder(ctx, exchange.OrderRequest{
			Symbol:        order.Symbol,
			Side:          side,
			Type:          orderType,
			Quantity:      quantity,
			QuoteOrderQty: quoteQty,
		})
		if err != nil {
			m.failOrder(ctx, order, fmt.Sprintf("Order execution failed: %v", err))
			return
		}
	}

	// Record trade in database
	tradeID, err := m.recordTrade(ctx, order, binanceOrder, details)
	if err != nil {
		m.logger.Error("[CONDITIONAL] Failed to record trade", "error", err)
	}

	// Update order as executed
	now := time.Now()
	_, err = m.pool.Exec(ctx, `
		UPDATE conditional_orders
		SET status = 'executed', executed_at = $2, result_trade_id = $3, result_order_id = $4
		WHERE id = $1
	`, order.ID, now, tradeID, binanceOrder.OrderID)

	if err != nil {
		m.logger.Error("[CONDITIONAL] Failed to update executed status", "error", err)
	}

	m.logEvent(ctx, order.ID, "executed", map[string]interface{}{
		"binance_order_id": binanceOrder.OrderID,
		"trade_id":         tradeID,
	})

	// Handle follow-up actions (trailing stop, TP/SL)
	if order.FollowUp != nil && tradeID != nil {
		m.setupFollowUp(ctx, order, *tradeID, details.Price)
	}

	m.logger.Info("[CONDITIONAL] Order executed successfully",
		"order_id", order.ID,
		"binance_order_id", binanceOrder.OrderID,
	)
}

// recordTrade records the executed trade
func (m *Monitor) recordTrade(ctx context.Context, order *ConditionalOrder, result *exchange.OrderResult, details TriggerDetails) (*string, error) {
	var tradeID string
	err := m.pool.QueryRow(ctx, `
		INSERT INTO trades (
			user_id, symbol, side, order_type, quantity, filled_price, total,
			status, binance_order_id, tradecore_order_id, notification_sent, notes, filled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, false, $11, NOW())
		RETURNING id
	`,
		order.UserID,
		order.Symbol,
		order.Action.Side,
		order.Action.Type,
		result.ExecutedQty,
		details.Price,
		0, // Will be calculated
		"filled",
		result.OrderID,
		fmt.Sprintf("conditional_%s", order.ID),
		fmt.Sprintf("Conditional order: %s", order.ID),
	).Scan(&tradeID)

	if err != nil {
		return nil, err
	}

	return &tradeID, nil
}

// setupFollowUp sets up follow-up actions (trailing stop, TP/SL)
func (m *Monitor) setupFollowUp(ctx context.Context, order *ConditionalOrder, tradeID string, fillPrice float64) {
	if order.FollowUp == nil {
		return
	}

	// Update trade with follow-up settings
	if order.FollowUp.StopLoss != nil || order.FollowUp.TakeProfit != nil || order.FollowUp.TrailingStop != nil {
		var trailingPct *float64
		var trailingSL *float64
		var trailingHighest *float64

		if order.FollowUp.TrailingStop != nil {
			trailingPct = &order.FollowUp.TrailingStop.CallbackPct
			trailingSL = &order.FollowUp.TrailingStop.InitialSL
			trailingHighest = &fillPrice
		}

		_, err := m.pool.Exec(ctx, `
			UPDATE trades
			SET stop_loss_price = $2,
			    take_profit_price = $3,
			    trailing_stop_pct = $4,
			    trailing_stop_price = $5,
			    trailing_stop_highest = $6
			WHERE id = $1
		`,
			tradeID,
			order.FollowUp.StopLoss,
			order.FollowUp.TakeProfit,
			trailingPct,
			trailingSL,
			trailingHighest,
		)

		if err != nil {
			m.logger.Error("[CONDITIONAL] Failed to setup follow-up", "error", err)
		} else {
			m.logger.Info("[CONDITIONAL] Follow-up configured",
				"trade_id", tradeID,
				"trailing_stop", order.FollowUp.TrailingStop != nil,
				"stop_loss", order.FollowUp.StopLoss,
				"take_profit", order.FollowUp.TakeProfit,
			)
		}
	}
}

// failOrder marks an order as failed
func (m *Monitor) failOrder(ctx context.Context, order *ConditionalOrder, errorMsg string) {
	m.logger.Error("[CONDITIONAL] Order failed",
		"order_id", order.ID,
		"error", errorMsg,
	)

	m.pool.Exec(ctx, `
		UPDATE conditional_orders
		SET status = 'failed', error_message = $2, retry_count = retry_count + 1
		WHERE id = $1
	`, order.ID, errorMsg)

	m.logEvent(ctx, order.ID, "failed", map[string]interface{}{
		"error": errorMsg,
	})

	m.removeOrder(order)
}

// expireOrder marks an order as expired
func (m *Monitor) expireOrder(ctx context.Context, order *ConditionalOrder) {
	m.logger.Info("[CONDITIONAL] Order expired", "order_id", order.ID)

	m.pool.Exec(ctx, `UPDATE conditional_orders SET status = 'expired' WHERE id = $1`, order.ID)
	m.logEvent(ctx, order.ID, "expired", nil)
	m.removeOrder(order)
}

// removeOrder removes an order from active monitoring
func (m *Monitor) removeOrder(order *ConditionalOrder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	orders := m.ordersBySymbol[order.Symbol]
	for i, o := range orders {
		if o.ID == order.ID {
			m.ordersBySymbol[order.Symbol] = append(orders[:i], orders[i+1:]...)
			break
		}
	}
}

// AddOrder adds a new order to monitoring
func (m *Monitor) AddOrder(order *ConditionalOrder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ordersBySymbol[order.Symbol] = append(m.ordersBySymbol[order.Symbol], order)

	m.logger.Info("[CONDITIONAL] Order added to monitoring",
		"order_id", order.ID,
		"symbol", order.Symbol,
	)
}

// logEvent logs an order event
func (m *Monitor) logEvent(ctx context.Context, orderID, eventType string, data map[string]interface{}) {
	dataJSON, _ := json.Marshal(data)
	m.pool.Exec(ctx, `
		INSERT INTO conditional_order_events (order_id, event_type, event_data)
		VALUES ($1, $2, $3)
	`, orderID, eventType, dataJSON)
}
