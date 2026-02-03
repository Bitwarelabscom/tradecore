package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"tradecore/internal/exchange"
	"tradecore/internal/persistence"
	"tradecore/internal/types"
)

// Engine is the core trading logic engine using the fan-in pattern
type Engine struct {
	logger      *slog.Logger
	executor    exchange.Executor
	streamer    exchange.MarketStreamer
	persistence *StatePersistence

	// PostgreSQL mode fields
	postgresPersistence *persistence.PostgresPersistence
	postgresMode        bool
	mockMode            bool
	userExecutors       map[string]exchange.Executor // userID -> executor
	userExecutorsMu     sync.RWMutex

	inputChan   chan types.Event
	activeBots  map[string]*types.BotState // keyed by botID in postgres mode, symbol in legacy mode
	mu          sync.RWMutex

	stopChan    chan struct{}
	persistStop chan struct{}
}

// NewEngine creates a new trading engine (legacy file persistence mode)
func NewEngine(
	executor exchange.Executor,
	streamer exchange.MarketStreamer,
	persistence *StatePersistence,
	logger *slog.Logger,
) *Engine {
	return &Engine{
		logger:        logger,
		executor:      executor,
		streamer:      streamer,
		persistence:   persistence,
		postgresMode:  false,
		userExecutors: make(map[string]exchange.Executor),
		inputChan:     make(chan types.Event, 1000),
		activeBots:    make(map[string]*types.BotState),
		stopChan:      make(chan struct{}),
		persistStop:   make(chan struct{}),
	}
}

// NewEngineWithPostgres creates a new trading engine with PostgreSQL persistence (multi-user mode)
func NewEngineWithPostgres(
	defaultExecutor exchange.Executor,
	streamer exchange.MarketStreamer,
	postgresPersistence *persistence.PostgresPersistence,
	logger *slog.Logger,
	mockMode bool,
) *Engine {
	return &Engine{
		logger:              logger,
		executor:            defaultExecutor, // May be nil in multi-user mode
		streamer:            streamer,
		postgresPersistence: postgresPersistence,
		postgresMode:        true,
		mockMode:            mockMode,
		userExecutors:       make(map[string]exchange.Executor),
		inputChan:           make(chan types.Event, 1000),
		activeBots:          make(map[string]*types.BotState),
		stopChan:            make(chan struct{}),
		persistStop:         make(chan struct{}),
	}
}

// InputChannel returns the input channel for sending events to the engine
func (e *Engine) InputChannel() chan<- types.Event {
	return e.inputChan
}

// Start initializes and starts the engine
func (e *Engine) Start(ctx context.Context) error {
	if e.postgresMode {
		return e.startPostgresMode(ctx)
	}
	return e.startLegacyMode(ctx)
}

// startPostgresMode initializes the engine in PostgreSQL multi-user mode
func (e *Engine) startPostgresMode(ctx context.Context) error {
	// Load active bots from PostgreSQL
	if e.postgresPersistence != nil {
		bots, err := e.postgresPersistence.LoadActiveBots(ctx)
		if err != nil {
			e.logger.Error("[ENGINE] Failed to load active bots from PostgreSQL", "error", err)
		} else {
			e.mu.Lock()
			e.activeBots = bots
			e.mu.Unlock()

			// Re-subscribe to symbols for active bots
			for botID, bot := range bots {
				if e.isBotActive(bot) {
					if err := e.streamer.Subscribe(ctx, bot.Symbol); err != nil {
						e.logger.Error("[ENGINE] Failed to resubscribe",
							"bot_id", botID,
							"symbol", bot.Symbol,
							"error", err,
						)
					} else {
						e.logger.Info("[ENGINE] Restored bot from PostgreSQL",
							"bot_id", botID,
							"symbol", bot.Symbol,
							"type", bot.BotType,
						)
					}
				}
			}
		}
	}

	// Start PostgreSQL periodic save goroutine
	if e.postgresPersistence != nil {
		go e.postgresPersistence.StartPeriodicSave(e.getBotsSnapshot, e.persistStop)
	}

	// Start main event loop
	go e.run(ctx)

	// Start forwarding events from streamer to input channel
	go e.forwardStreamerEvents(ctx)

	e.logger.Info("[ENGINE] Started in PostgreSQL mode")
	return nil
}

// startLegacyMode initializes the engine in legacy file persistence mode
func (e *Engine) startLegacyMode(ctx context.Context) error {
	// Load persisted state
	if e.persistence != nil {
		bots, err := e.persistence.Load()
		if err != nil {
			e.logger.Error("[ENGINE] Failed to load state", "error", err)
		} else {
			e.mu.Lock()
			e.activeBots = bots
			e.mu.Unlock()

			// Re-subscribe to symbols for active bots
			for symbol, bot := range bots {
				if (bot.Grid != nil && bot.Grid.Active) || (bot.TrailingStop != nil && bot.TrailingStop.Active) {
					if err := e.streamer.Subscribe(ctx, symbol); err != nil {
						e.logger.Error("[ENGINE] Failed to resubscribe",
							"symbol", symbol,
							"error", err,
						)
					} else {
						e.logger.Info("[ENGINE] Restored bot",
							"symbol", symbol,
							"has_grid", bot.Grid != nil && bot.Grid.Active,
							"has_trailing_stop", bot.TrailingStop != nil && bot.TrailingStop.Active,
						)
					}
				}
			}
		}
	}

	// Start persistence goroutine
	if e.persistence != nil {
		go e.persistence.StartPeriodicSave(e.getBotsSnapshot, e.persistStop)
	}

	// Start main event loop
	go e.run(ctx)

	// Start forwarding events from streamer to input channel
	go e.forwardStreamerEvents(ctx)

	e.logger.Info("[ENGINE] Started in legacy mode")
	return nil
}

// isBotActive checks if a bot has any active strategy
func (e *Engine) isBotActive(bot *types.BotState) bool {
	if bot.Grid != nil && bot.Grid.Active {
		return true
	}
	if bot.TrailingStop != nil && bot.TrailingStop.Active {
		return true
	}
	if bot.DCA != nil && bot.DCA.Active {
		return true
	}
	if bot.RSI != nil && bot.RSI.Active {
		return true
	}
	if bot.MACrossover != nil && bot.MACrossover.Active {
		return true
	}
	if bot.MACD != nil && bot.MACD.Active {
		return true
	}
	if bot.Breakout != nil && bot.Breakout.Active {
		return true
	}
	if bot.MeanReversion != nil && bot.MeanReversion.Active {
		return true
	}
	if bot.Momentum != nil && bot.Momentum.Active {
		return true
	}
	return false
}

// forwardStreamerEvents forwards events from the market streamer to the input channel
func (e *Engine) forwardStreamerEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopChan:
			return
		case event, ok := <-e.streamer.Events():
			if !ok {
				return
			}
			select {
			case e.inputChan <- event:
			default:
				e.logger.Warn("[ENGINE] Input channel full, dropping event")
			}
		}
	}
}

// run is the main event loop using the fan-in pattern
func (e *Engine) run(ctx context.Context) {
	e.logger.Info("[ENGINE] Event loop started")

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("[ENGINE] Context cancelled, shutting down")
			return
		case <-e.stopChan:
			e.logger.Info("[ENGINE] Stop signal received")
			return
		case event := <-e.inputChan:
			e.handleEvent(ctx, event)
		}
	}
}

// handleEvent processes a single event
func (e *Engine) handleEvent(ctx context.Context, event types.Event) {
	switch event.Type {
	case types.EventTypeSignal:
		if event.Signal != nil {
			e.handleSignal(ctx, event.Signal)
		}
	case types.EventTypePriceUpdate:
		if event.PriceUpdate != nil {
			e.handlePriceUpdate(ctx, event.PriceUpdate)
		}
	case types.EventTypeBotCommand:
		if event.BotCommand != nil {
			e.handleBotCommand(ctx, event.BotCommand)
		}
	}
}

// handleBotCommand processes bot start/stop commands (PostgreSQL mode)
func (e *Engine) handleBotCommand(ctx context.Context, cmd *types.BotCommand) {
	e.logger.Info("[ENGINE] Processing bot command",
		"command", cmd.Command,
		"bot_id", cmd.BotID,
		"user_id", cmd.UserID,
		"type", cmd.BotType,
		"symbol", cmd.Symbol,
	)

	switch cmd.Command {
	case "start":
		e.startBotFromCommand(ctx, cmd)
	case "stop":
		e.stopBotFromCommand(ctx, cmd)
	case "update":
		e.updateBotFromCommand(ctx, cmd)
	default:
		e.logger.Error("[ENGINE] Unknown bot command", "command", cmd.Command)
	}
}

// startBotFromCommand starts a bot from an HTTP command
func (e *Engine) startBotFromCommand(ctx context.Context, cmd *types.BotCommand) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Load bot from database if not already loaded
	bot, exists := e.activeBots[cmd.BotID]
	if !exists {
		if e.postgresPersistence != nil {
			var err error
			bot, _, err = e.postgresPersistence.LoadBot(ctx, cmd.BotID)
			if err != nil {
				e.logger.Error("[ENGINE] Failed to load bot from database",
					"bot_id", cmd.BotID,
					"error", err,
				)
				return
			}
		} else {
			// Create new bot state
			bot = &types.BotState{
				BotID:     cmd.BotID,
				UserID:    cmd.UserID,
				Symbol:    cmd.Symbol,
				BotType:   cmd.BotType,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
		}
		e.activeBots[cmd.BotID] = bot
	}

	// Subscribe to market data for this symbol
	if err := e.streamer.Subscribe(ctx, cmd.Symbol); err != nil {
		e.logger.Error("[ENGINE] Failed to subscribe",
			"symbol", cmd.Symbol,
			"error", err,
		)
		return
	}

	// Update status in database
	if e.postgresPersistence != nil {
		if err := e.postgresPersistence.UpdateBotStatus(ctx, cmd.BotID, "running", ""); err != nil {
			e.logger.Error("[ENGINE] Failed to update bot status",
				"bot_id", cmd.BotID,
				"error", err,
			)
		}
	}

	e.logger.Info("[ENGINE] Bot started",
		"bot_id", cmd.BotID,
		"symbol", cmd.Symbol,
		"type", cmd.BotType,
	)
}

// stopBotFromCommand stops a bot from an HTTP command
func (e *Engine) stopBotFromCommand(ctx context.Context, cmd *types.BotCommand) {
	e.mu.Lock()
	defer e.mu.Unlock()

	bot, exists := e.activeBots[cmd.BotID]
	if !exists {
		e.logger.Warn("[ENGINE] Bot not found to stop", "bot_id", cmd.BotID)
		return
	}

	// Deactivate all strategies
	if bot.Grid != nil {
		bot.Grid.Active = false
	}
	if bot.TrailingStop != nil {
		bot.TrailingStop.Active = false
	}
	if bot.DCA != nil {
		bot.DCA.Active = false
	}
	if bot.RSI != nil {
		bot.RSI.Active = false
	}
	if bot.MACrossover != nil {
		bot.MACrossover.Active = false
	}
	if bot.MACD != nil {
		bot.MACD.Active = false
	}
	if bot.Breakout != nil {
		bot.Breakout.Active = false
	}
	if bot.MeanReversion != nil {
		bot.MeanReversion.Active = false
	}
	if bot.Momentum != nil {
		bot.Momentum.Active = false
	}

	bot.UpdatedAt = time.Now()

	// Update status in database
	if e.postgresPersistence != nil {
		if err := e.postgresPersistence.UpdateBotStatus(ctx, cmd.BotID, "stopped", ""); err != nil {
			e.logger.Error("[ENGINE] Failed to update bot status",
				"bot_id", cmd.BotID,
				"error", err,
			)
		}
		// Save final state
		if err := e.postgresPersistence.SaveBotState(ctx, cmd.BotID, bot); err != nil {
			e.logger.Error("[ENGINE] Failed to save bot state",
				"bot_id", cmd.BotID,
				"error", err,
			)
		}
	}

	// Remove from active bots
	delete(e.activeBots, cmd.BotID)

	// Check if we should unsubscribe from this symbol
	e.maybeUnsubscribeSymbol(ctx, bot.Symbol)

	e.logger.Info("[ENGINE] Bot stopped", "bot_id", cmd.BotID)
}

// updateBotFromCommand updates a bot's configuration
func (e *Engine) updateBotFromCommand(ctx context.Context, cmd *types.BotCommand) {
	e.mu.Lock()
	defer e.mu.Unlock()

	bot, exists := e.activeBots[cmd.BotID]
	if !exists {
		e.logger.Warn("[ENGINE] Bot not found to update", "bot_id", cmd.BotID)
		return
	}

	// TODO: Parse and apply config updates based on bot type
	bot.UpdatedAt = time.Now()

	if e.postgresPersistence != nil {
		if err := e.postgresPersistence.SaveBotState(ctx, cmd.BotID, bot); err != nil {
			e.logger.Error("[ENGINE] Failed to save updated bot state",
				"bot_id", cmd.BotID,
				"error", err,
			)
		}
	}

	e.logger.Info("[ENGINE] Bot updated", "bot_id", cmd.BotID)
}

// maybeUnsubscribeSymbol checks if any bot still uses a symbol and unsubscribes if not
func (e *Engine) maybeUnsubscribeSymbol(ctx context.Context, symbol string) {
	for _, bot := range e.activeBots {
		if bot.Symbol == symbol && e.isBotActive(bot) {
			return // Still have an active bot using this symbol
		}
	}
	e.streamer.Unsubscribe(symbol)
}

// getOrCreateUserExecutor gets or creates an executor for a user (multi-user mode)
func (e *Engine) getOrCreateUserExecutor(ctx context.Context, userID string) (exchange.Executor, error) {
	// In mock mode, use the default mock executor
	if e.mockMode && e.executor != nil {
		return e.executor, nil
	}

	e.userExecutorsMu.RLock()
	exec, exists := e.userExecutors[userID]
	e.userExecutorsMu.RUnlock()

	if exists {
		return exec, nil
	}

	// Load credentials and create new executor
	if e.postgresPersistence == nil {
		return nil, nil
	}

	creds, err := e.postgresPersistence.GetUserCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Create new Binance client for this user
	newExec := exchange.NewBinanceClient(creds.APIKey, creds.APISecret, e.logger)

	e.userExecutorsMu.Lock()
	e.userExecutors[userID] = newExec
	e.userExecutorsMu.Unlock()

	e.logger.Info("[ENGINE] Created executor for user", "user_id", userID)
	return newExec, nil
}

// handleSignal processes incoming trading signals
func (e *Engine) handleSignal(ctx context.Context, signal *types.SignalEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.logger.Info("[ENGINE] Processing signal",
		"symbol", signal.Symbol,
		"action", signal.Action,
	)

	switch signal.Action {
	case types.ActionStartGrid:
		e.startGrid(ctx, signal)
	case types.ActionStopGrid:
		e.stopGrid(ctx, signal.Symbol)
	case types.ActionStartTrailingStop:
		e.startTrailingStop(ctx, signal)
	case types.ActionStopTrailingStop:
		e.stopTrailingStop(ctx, signal.Symbol)
	}

	if e.persistence != nil {
		e.persistence.MarkDirty()
	}
}

// startGrid initializes a grid bot for a symbol
func (e *Engine) startGrid(ctx context.Context, signal *types.SignalEvent) {
	cfg := signal.GridConfig
	if cfg == nil {
		e.logger.Error("[ENGINE] Grid config missing", "symbol", signal.Symbol)
		return
	}

	// Calculate grid step
	gridStep := (cfg.UpperPrice - cfg.LowerPrice) / float64(cfg.GridLines)

	// Initialize grid levels
	levels := make([]types.GridLevel, cfg.GridLines+1)
	for i := 0; i <= cfg.GridLines; i++ {
		levels[i] = types.GridLevel{
			Price:       cfg.LowerPrice + float64(i)*gridStep,
			IsBuyFilled: false,
			Quantity:    cfg.Quantity,
		}
	}

	// Get or create bot state
	bot := e.getOrCreateBot(signal.Symbol)
	bot.Grid = &types.GridState{
		Active:     true,
		UpperPrice: cfg.UpperPrice,
		LowerPrice: cfg.LowerPrice,
		GridStep:   gridStep,
		Levels:     levels,
		LastPrice:  0,
	}
	bot.UpdatedAt = time.Now()

	// Subscribe to market data
	if err := e.streamer.Subscribe(ctx, signal.Symbol); err != nil {
		e.logger.Error("[ENGINE] Failed to subscribe",
			"symbol", signal.Symbol,
			"error", err,
		)
		return
	}

	e.logger.Info("[ENGINE] Grid bot started",
		"symbol", signal.Symbol,
		"upper", cfg.UpperPrice,
		"lower", cfg.LowerPrice,
		"grid_lines", cfg.GridLines,
		"grid_step", gridStep,
		"quantity", cfg.Quantity,
	)
}

// stopGrid stops the grid bot for a symbol
func (e *Engine) stopGrid(ctx context.Context, symbol string) {
	bot, exists := e.activeBots[symbol]
	if !exists || bot.Grid == nil {
		e.logger.Warn("[ENGINE] No grid bot to stop", "symbol", symbol)
		return
	}

	bot.Grid.Active = false
	bot.UpdatedAt = time.Now()

	// Check if we should unsubscribe
	e.maybeUnsubscribe(ctx, symbol)

	e.logger.Info("[ENGINE] Grid bot stopped", "symbol", symbol)
}

// startTrailingStop initializes a trailing stop for a symbol
func (e *Engine) startTrailingStop(ctx context.Context, signal *types.SignalEvent) {
	cfg := signal.SLConfig
	if cfg == nil {
		e.logger.Error("[ENGINE] Trailing stop config missing", "symbol", signal.Symbol)
		return
	}

	bot := e.getOrCreateBot(signal.Symbol)
	bot.TrailingStop = &types.TrailingStopState{
		Active:          true,
		ActivationPrice: cfg.ActivationPrice,
		CallbackRate:    cfg.CallbackRate,
		Quantity:        cfg.Quantity,
		HighestPrice:    0,
		Activated:       cfg.ActivationPrice == 0, // Activate immediately if no activation price
	}
	bot.UpdatedAt = time.Now()

	// Subscribe to market data
	if err := e.streamer.Subscribe(ctx, signal.Symbol); err != nil {
		e.logger.Error("[ENGINE] Failed to subscribe",
			"symbol", signal.Symbol,
			"error", err,
		)
		return
	}

	e.logger.Info("[ENGINE] Trailing stop started",
		"symbol", signal.Symbol,
		"activation_price", cfg.ActivationPrice,
		"callback_rate", cfg.CallbackRate,
		"quantity", cfg.Quantity,
	)
}

// stopTrailingStop stops the trailing stop for a symbol
func (e *Engine) stopTrailingStop(ctx context.Context, symbol string) {
	bot, exists := e.activeBots[symbol]
	if !exists || bot.TrailingStop == nil {
		e.logger.Warn("[ENGINE] No trailing stop to stop", "symbol", symbol)
		return
	}

	bot.TrailingStop.Active = false
	bot.UpdatedAt = time.Now()

	// Check if we should unsubscribe
	e.maybeUnsubscribe(ctx, symbol)

	e.logger.Info("[ENGINE] Trailing stop stopped", "symbol", symbol)
}

// maybeUnsubscribe unsubscribes from market data if no active bots for symbol
func (e *Engine) maybeUnsubscribe(ctx context.Context, symbol string) {
	bot, exists := e.activeBots[symbol]
	if !exists {
		e.streamer.Unsubscribe(symbol)
		return
	}

	gridActive := bot.Grid != nil && bot.Grid.Active
	tsActive := bot.TrailingStop != nil && bot.TrailingStop.Active

	if !gridActive && !tsActive {
		e.streamer.Unsubscribe(symbol)
	}
}

// getOrCreateBot gets an existing bot state or creates a new one
func (e *Engine) getOrCreateBot(symbol string) *types.BotState {
	bot, exists := e.activeBots[symbol]
	if !exists {
		bot = &types.BotState{
			Symbol:    symbol,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		e.activeBots[symbol] = bot
	}
	return bot
}

// handlePriceUpdate processes real-time price updates
func (e *Engine) handlePriceUpdate(ctx context.Context, update *types.PriceUpdate) {
	e.mu.Lock()
	defer e.mu.Unlock()

	bot, exists := e.activeBots[update.Symbol]
	if !exists {
		return
	}

	// Process grid logic
	if bot.Grid != nil && bot.Grid.Active {
		e.processGridLogic(ctx, bot, update.Price)
	}

	// Process trailing stop logic
	if bot.TrailingStop != nil && bot.TrailingStop.Active {
		e.processTrailingStopLogic(ctx, bot, update.Price)
	}
}

// processGridLogic handles grid bot price crosses
func (e *Engine) processGridLogic(ctx context.Context, bot *types.BotState, currentPrice float64) {
	grid := bot.Grid
	lastPrice := grid.LastPrice

	// Skip if this is the first price update
	if lastPrice == 0 {
		grid.LastPrice = currentPrice
		return
	}

	// Check each grid level for crosses
	for i := range grid.Levels {
		level := &grid.Levels[i]
		levelPrice := level.Price

		// Price crossed down through this level (BUY signal)
		if lastPrice > levelPrice && currentPrice <= levelPrice && !level.IsBuyFilled {
			e.executeGridBuy(ctx, bot.Symbol, level, i, grid)
		}

		// Price crossed up through this level (SELL signal for filled buys)
		if lastPrice < levelPrice && currentPrice >= levelPrice && level.IsBuyFilled {
			e.executeGridSell(ctx, bot.Symbol, level, i, grid)
		}
	}

	grid.LastPrice = currentPrice
}

// executeGridBuy executes a grid buy order
func (e *Engine) executeGridBuy(ctx context.Context, symbol string, level *types.GridLevel, levelIdx int, grid *types.GridState) {
	req := types.OrderRequest{
		Symbol:   symbol,
		Side:     types.SideBuy,
		Quantity: level.Quantity,
		Price:    level.Price,
	}

	result, err := e.executor.ExecuteOrder(ctx, req)
	if err != nil {
		e.logger.Error("[ENGINE] Grid buy order failed",
			"symbol", symbol,
			"level", levelIdx,
			"price", level.Price,
			"error", err,
		)
		return
	}

	if result.Success {
		level.IsBuyFilled = true
		e.logger.Info("[ENGINE] Grid Buy Filled",
			"symbol", symbol,
			"level", levelIdx,
			"price", level.Price,
			"quantity", level.Quantity,
			"order_id", result.OrderID,
		)

		if e.persistence != nil {
			e.persistence.MarkDirty()
		}
	}
}

// executeGridSell executes a grid sell order
func (e *Engine) executeGridSell(ctx context.Context, symbol string, level *types.GridLevel, levelIdx int, grid *types.GridState) {
	req := types.OrderRequest{
		Symbol:   symbol,
		Side:     types.SideSell,
		Quantity: level.Quantity,
		Price:    level.Price,
	}

	result, err := e.executor.ExecuteOrder(ctx, req)
	if err != nil {
		e.logger.Error("[ENGINE] Grid sell order failed",
			"symbol", symbol,
			"level", levelIdx,
			"price", level.Price,
			"error", err,
		)
		return
	}

	if result.Success {
		level.IsBuyFilled = false // Ready to buy again
		e.logger.Info("[ENGINE] Grid Sell Filled",
			"symbol", symbol,
			"level", levelIdx,
			"price", level.Price,
			"quantity", level.Quantity,
			"order_id", result.OrderID,
		)

		if e.persistence != nil {
			e.persistence.MarkDirty()
		}
	}
}

// processTrailingStopLogic handles trailing stop price tracking
func (e *Engine) processTrailingStopLogic(ctx context.Context, bot *types.BotState, currentPrice float64) {
	ts := bot.TrailingStop

	// Check if we need to activate
	if !ts.Activated {
		if ts.ActivationPrice > 0 && currentPrice >= ts.ActivationPrice {
			ts.Activated = true
			ts.HighestPrice = currentPrice
			e.logger.Info("[ENGINE] Trailing stop activated",
				"symbol", bot.Symbol,
				"activation_price", ts.ActivationPrice,
				"current_price", currentPrice,
			)
		}
		return
	}

	// Update highest price if current is higher
	if currentPrice > ts.HighestPrice {
		ts.HighestPrice = currentPrice
		e.logger.Debug("[ENGINE] Trailing stop highest price updated",
			"symbol", bot.Symbol,
			"highest_price", ts.HighestPrice,
		)
	}

	// Calculate stop price
	stopPrice := ts.HighestPrice * (1 - ts.CallbackRate)

	// Check if stop triggered
	if currentPrice <= stopPrice {
		e.executeTrailingStopSell(ctx, bot, currentPrice, stopPrice)
	}
}

// executeTrailingStopSell executes the trailing stop sell order
func (e *Engine) executeTrailingStopSell(ctx context.Context, bot *types.BotState, currentPrice, stopPrice float64) {
	ts := bot.TrailingStop

	req := types.OrderRequest{
		Symbol:   bot.Symbol,
		Side:     types.SideSell,
		Quantity: ts.Quantity,
		Price:    currentPrice, // Sell at current market price
	}

	result, err := e.executor.ExecuteOrder(ctx, req)
	if err != nil {
		e.logger.Error("[ENGINE] Trailing stop sell failed",
			"symbol", bot.Symbol,
			"error", err,
		)
		return
	}

	if result.Success {
		ts.Active = false // Stop is triggered, deactivate
		bot.UpdatedAt = time.Now()

		e.logger.Info("[ENGINE] Trailing Stop Triggered",
			"symbol", bot.Symbol,
			"highest_price", ts.HighestPrice,
			"stop_price", stopPrice,
			"executed_price", currentPrice,
			"quantity", ts.Quantity,
			"order_id", result.OrderID,
		)

		if e.persistence != nil {
			e.persistence.MarkDirty()
		}

		// Check if we should unsubscribe
		e.maybeUnsubscribe(ctx, bot.Symbol)
	}
}

// Stop gracefully stops the engine
func (e *Engine) Stop() {
	e.logger.Info("[ENGINE] Stopping...")
	close(e.stopChan)

	// Stop persistence
	if e.persistence != nil {
		close(e.persistStop)
	}
}

// getBotsSnapshot returns a copy of the active bots map (for persistence)
func (e *Engine) getBotsSnapshot() map[string]*types.BotState {
	e.mu.RLock()
	defer e.mu.RUnlock()

	snapshot := make(map[string]*types.BotState, len(e.activeBots))
	for k, v := range e.activeBots {
		// Deep copy would be better in production
		snapshot[k] = v
	}
	return snapshot
}

// GetActiveBots returns information about active bots (for status endpoints)
func (e *Engine) GetActiveBots() map[string]*types.BotState {
	return e.getBotsSnapshot()
}
