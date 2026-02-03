package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tradecore/internal/crypto"
	"tradecore/internal/types"
)

// PostgresPersistence handles state persistence to PostgreSQL
type PostgresPersistence struct {
	pool          *pgxpool.Pool
	logger        *slog.Logger
	encryptionKey string
	mu            sync.Mutex
	saveInterval  time.Duration
	dirty         bool
	lastSave      time.Time
}

// UserCredentials holds decrypted Binance API credentials
type UserCredentials struct {
	APIKey    string
	APISecret string
}

// BotRecord represents a bot from the database
type BotRecord struct {
	ID            string
	UserID        string
	Name          string
	Type          string
	Symbol        string
	Config        json.RawMessage
	Status        string
	TradecoreState json.RawMessage
}

// NewPostgresPersistence creates a new PostgreSQL persistence handler
func NewPostgresPersistence(ctx context.Context, logger *slog.Logger) (*PostgresPersistence, error) {
	// Load encryption key
	encryptionKey, err := crypto.LoadEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load encryption key: %w", err)
	}

	// Build connection string
	connStr := buildConnectionString()
	logger.Info("[POSTGRES] Connecting to database", "host", os.Getenv("POSTGRES_HOST"))

	// Create connection pool
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 30 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Info("[POSTGRES] Connected to database")

	return &PostgresPersistence{
		pool:          pool,
		logger:        logger,
		encryptionKey: encryptionKey,
		saveInterval:  30 * time.Second,
	}, nil
}

// buildConnectionString creates a PostgreSQL connection string from environment variables
func buildConnectionString() string {
	host := getEnvOrDefault("POSTGRES_HOST", "localhost")
	port := getEnvOrDefault("POSTGRES_PORT", "5432")
	user := getEnvOrDefault("POSTGRES_USER", "luna")
	dbname := getEnvOrDefault("POSTGRES_DB", "luna_chat")

	// Try to read password from Docker secret first
	password := ""
	if data, err := os.ReadFile("/run/secrets/postgres_password"); err == nil {
		password = strings.TrimSpace(string(data))
	} else {
		password = os.Getenv("POSTGRES_PASSWORD")
	}

	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname,
	)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Close closes the database connection pool
func (p *PostgresPersistence) Close() {
	if p.pool != nil {
		p.pool.Close()
		p.logger.Info("[POSTGRES] Connection closed")
	}
}

// LoadActiveBots loads all bots that are marked as tradecore_managed and running
func (p *PostgresPersistence) LoadActiveBots(ctx context.Context) (map[string]*types.BotState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	query := `
		SELECT id, user_id, name, type, symbol, config, status, tradecore_state
		FROM trading_bots
		WHERE tradecore_managed = true AND status = 'running'
	`

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active bots: %w", err)
	}
	defer rows.Close()

	bots := make(map[string]*types.BotState)
	for rows.Next() {
		var rec BotRecord
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.Name, &rec.Type, &rec.Symbol, &rec.Config, &rec.Status, &rec.TradecoreState); err != nil {
			p.logger.Error("[POSTGRES] Failed to scan bot row", "error", err)
			continue
		}

		botState, err := p.recordToBotState(&rec)
		if err != nil {
			p.logger.Error("[POSTGRES] Failed to convert bot record", "bot_id", rec.ID, "error", err)
			continue
		}

		// Key by bot ID (not symbol) to support multiple bots per symbol
		bots[rec.ID] = botState
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating bot rows: %w", err)
	}

	p.logger.Info("[POSTGRES] Loaded active bots", "count", len(bots))
	return bots, nil
}

// LoadBot loads a single bot by ID
func (p *PostgresPersistence) LoadBot(ctx context.Context, botID string) (*types.BotState, *BotRecord, error) {
	query := `
		SELECT id, user_id, name, type, symbol, config, status, tradecore_state
		FROM trading_bots
		WHERE id = $1
	`

	var rec BotRecord
	err := p.pool.QueryRow(ctx, query, botID).Scan(
		&rec.ID, &rec.UserID, &rec.Name, &rec.Type, &rec.Symbol, &rec.Config, &rec.Status, &rec.TradecoreState,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, fmt.Errorf("bot not found: %s", botID)
		}
		return nil, nil, fmt.Errorf("failed to load bot: %w", err)
	}

	botState, err := p.recordToBotState(&rec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to convert bot record: %w", err)
	}

	return botState, &rec, nil
}

// recordToBotState converts a database record to a BotState
func (p *PostgresPersistence) recordToBotState(rec *BotRecord) (*types.BotState, error) {
	botState := &types.BotState{
		BotID:     rec.ID,
		UserID:    rec.UserID,
		Symbol:    rec.Symbol,
		BotType:   rec.Type,
		CreatedAt: time.Now(), // Will be overwritten if tradecore_state exists
		UpdatedAt: time.Now(),
	}

	// Parse tradecore_state if it exists
	if len(rec.TradecoreState) > 0 && string(rec.TradecoreState) != "null" {
		var state types.TradecoreStateJSON
		if err := json.Unmarshal(rec.TradecoreState, &state); err != nil {
			return nil, fmt.Errorf("failed to parse tradecore_state: %w", err)
		}

		botState.Grid = state.Grid
		botState.TrailingStop = state.TrailingStop
		botState.DCA = state.DCA
		botState.RSI = state.RSI
		botState.MACrossover = state.MACrossover
		botState.MACD = state.MACD
		botState.Breakout = state.Breakout
		botState.MeanReversion = state.MeanReversion
		botState.Momentum = state.Momentum

		if !state.CreatedAt.IsZero() {
			botState.CreatedAt = state.CreatedAt
		}
		if !state.UpdatedAt.IsZero() {
			botState.UpdatedAt = state.UpdatedAt
		}
	}

	return botState, nil
}

// SaveBotState saves the bot state to the database
func (p *PostgresPersistence) SaveBotState(ctx context.Context, botID string, state *types.BotState) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Convert state to JSON
	stateJSON := types.TradecoreStateJSON{
		Grid:           state.Grid,
		TrailingStop:   state.TrailingStop,
		DCA:            state.DCA,
		RSI:            state.RSI,
		MACrossover:    state.MACrossover,
		MACD:           state.MACD,
		Breakout:       state.Breakout,
		MeanReversion:  state.MeanReversion,
		Momentum:       state.Momentum,
		CreatedAt:      state.CreatedAt,
		UpdatedAt:      time.Now(),
	}

	jsonData, err := json.Marshal(stateJSON)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	query := `
		UPDATE trading_bots
		SET tradecore_state = $1, tradecore_managed = true, updated_at = NOW()
		WHERE id = $2
	`

	_, err = p.pool.Exec(ctx, query, jsonData, botID)
	if err != nil {
		return fmt.Errorf("failed to save bot state: %w", err)
	}

	p.lastSave = time.Now()
	p.dirty = false

	p.logger.Debug("[POSTGRES] Bot state saved", "bot_id", botID)
	return nil
}

// UpdateBotStatus updates the bot status in the database
func (p *PostgresPersistence) UpdateBotStatus(ctx context.Context, botID string, status string, lastError string) error {
	var query string
	var args []interface{}

	if lastError != "" {
		query = `
			UPDATE trading_bots
			SET status = $1, last_error = $2, updated_at = NOW()
			WHERE id = $3
		`
		args = []interface{}{status, lastError, botID}
	} else {
		query = `
			UPDATE trading_bots
			SET status = $1, last_error = NULL, updated_at = NOW()
			WHERE id = $2
		`
		args = []interface{}{status, botID}
	}

	// Add started_at or stopped_at based on status
	if status == "running" {
		query = `
			UPDATE trading_bots
			SET status = $1, last_error = NULL, started_at = NOW(), tradecore_managed = true, updated_at = NOW()
			WHERE id = $2
		`
		args = []interface{}{status, botID}
	} else if status == "stopped" {
		query = `
			UPDATE trading_bots
			SET status = $1, stopped_at = NOW(), tradecore_managed = false, updated_at = NOW()
			WHERE id = $2
		`
		args = []interface{}{status, botID}
	}

	_, err := p.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update bot status: %w", err)
	}

	p.logger.Info("[POSTGRES] Bot status updated", "bot_id", botID, "status", status)
	return nil
}

// RecordTrade inserts a new trade into the trades table
func (p *PostgresPersistence) RecordTrade(ctx context.Context, trade *types.TradeRecord) error {
	query := `
		INSERT INTO trades (
			user_id, bot_id, symbol, side, order_type, quantity, price, filled_price,
			total, fee, fee_asset, status, binance_order_id, tradecore_order_id,
			notification_sent, filled_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		) RETURNING id
	`

	var tradeID string
	err := p.pool.QueryRow(ctx, query,
		trade.UserID,
		trade.BotID,
		trade.Symbol,
		trade.Side,
		trade.OrderType,
		trade.Quantity,
		trade.Price,
		trade.FilledPrice,
		trade.Total,
		trade.Fee,
		trade.FeeAsset,
		trade.Status,
		trade.BinanceOrderID,
		trade.TradecoreOrderID,
		false, // notification_sent = false so Luna picks it up
		trade.FilledAt,
	).Scan(&tradeID)

	if err != nil {
		return fmt.Errorf("failed to record trade: %w", err)
	}

	p.logger.Info("[POSTGRES] Trade recorded",
		"trade_id", tradeID,
		"bot_id", trade.BotID,
		"symbol", trade.Symbol,
		"side", trade.Side,
		"quantity", trade.Quantity,
		"price", trade.FilledPrice,
	)

	return nil
}

// UpdateBotStats updates the bot's trading statistics
func (p *PostgresPersistence) UpdateBotStats(ctx context.Context, botID string, profit float64, isWin bool) error {
	var query string
	if isWin {
		query = `
			UPDATE trading_bots
			SET total_profit = total_profit + $1,
			    total_trades = total_trades + 1,
			    win_rate = (SELECT COALESCE(
			        CASE WHEN total_trades > 0
			        THEN (wins + 1)::DECIMAL / (total_trades + 1) * 100
			        ELSE 100 END, 100)
			        FROM (SELECT COUNT(*) FILTER (WHERE close_reason IN ('take_profit', 'trailing_stop')) as wins,
			                     COUNT(*) as total_trades
			              FROM trades WHERE bot_id = $2 AND status = 'filled') sub),
			    updated_at = NOW()
			WHERE id = $2
		`
	} else {
		query = `
			UPDATE trading_bots
			SET total_profit = total_profit + $1,
			    total_trades = total_trades + 1,
			    win_rate = (SELECT COALESCE(
			        CASE WHEN total_trades > 0
			        THEN wins::DECIMAL / (total_trades + 1) * 100
			        ELSE 0 END, 0)
			        FROM (SELECT COUNT(*) FILTER (WHERE close_reason IN ('take_profit', 'trailing_stop')) as wins,
			                     COUNT(*) as total_trades
			              FROM trades WHERE bot_id = $2 AND status = 'filled') sub),
			    updated_at = NOW()
			WHERE id = $2
		`
	}

	_, err := p.pool.Exec(ctx, query, profit, botID)
	if err != nil {
		return fmt.Errorf("failed to update bot stats: %w", err)
	}

	return nil
}

// GetUserCredentials retrieves and decrypts user's Binance API credentials
func (p *PostgresPersistence) GetUserCredentials(ctx context.Context, userID string) (*UserCredentials, error) {
	query := `
		SELECT api_key_encrypted, api_secret_encrypted
		FROM user_trading_keys
		WHERE user_id = $1
	`

	var apiKeyEnc, apiSecretEnc string
	err := p.pool.QueryRow(ctx, query, userID).Scan(&apiKeyEnc, &apiSecretEnc)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("no trading keys found for user: %s", userID)
		}
		return nil, fmt.Errorf("failed to get user credentials: %w", err)
	}

	// Decrypt API key
	apiKey, err := crypto.DecryptToken(apiKeyEnc, p.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	// Decrypt API secret
	apiSecret, err := crypto.DecryptToken(apiSecretEnc, p.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API secret: %w", err)
	}

	return &UserCredentials{
		APIKey:    apiKey,
		APISecret: apiSecret,
	}, nil
}

// LogBotAction logs a bot action to the bot_logs table
func (p *PostgresPersistence) LogBotAction(ctx context.Context, botID string, action string, details map[string]interface{}) error {
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("failed to marshal details: %w", err)
	}

	query := `
		INSERT INTO bot_logs (bot_id, action, details)
		VALUES ($1, $2, $3)
	`

	_, err = p.pool.Exec(ctx, query, botID, action, detailsJSON)
	if err != nil {
		return fmt.Errorf("failed to log bot action: %w", err)
	}

	return nil
}

// MarkDirty marks the state as needing to be saved
func (p *PostgresPersistence) MarkDirty() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dirty = true
}

// ShouldSave returns true if enough time has passed and state is dirty
func (p *PostgresPersistence) ShouldSave() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dirty && time.Since(p.lastSave) >= p.saveInterval
}

// StartPeriodicSave starts a goroutine that periodically saves state for all active bots
func (p *PostgresPersistence) StartPeriodicSave(getBots func() map[string]*types.BotState, stopChan <-chan struct{}) {
	ticker := time.NewTicker(p.saveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			// Final save on shutdown
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			bots := getBots()
			for botID, state := range bots {
				if err := p.SaveBotState(ctx, botID, state); err != nil {
					p.logger.Error("[POSTGRES] Failed to save bot state on shutdown",
						"bot_id", botID,
						"error", err,
					)
				}
			}
			cancel()
			p.logger.Info("[POSTGRES] Final state saved on shutdown")
			return

		case <-ticker.C:
			if p.ShouldSave() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				bots := getBots()
				for botID, state := range bots {
					if err := p.SaveBotState(ctx, botID, state); err != nil {
						p.logger.Error("[POSTGRES] Failed to save bot state",
							"bot_id", botID,
							"error", err,
						)
					}
				}
				cancel()
			}
		}
	}
}
