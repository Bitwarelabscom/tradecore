package receiver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"tradecore/internal/types"
)

// SignalRequest represents the incoming HTTP request body
type SignalRequest struct {
	Symbol     string                  `json:"symbol"`
	Action     string                  `json:"action"`
	GridConfig *types.GridConfig       `json:"grid_config,omitempty"`
	SLConfig   *types.TrailingStopConfig `json:"sl_config,omitempty"`
}

// HTTPReceiver handles incoming HTTP signals
type HTTPReceiver struct {
	server           *http.Server
	logger           *slog.Logger
	eventChan        chan<- types.Event
	port             int
	pool             Pool // Database pool for conditional orders
	conditionalMgr   ConditionalManager
}

// Pool interface for database operations
type Pool interface {
	Query(ctx context.Context, sql string, args ...interface{}) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) Row
	Exec(ctx context.Context, sql string, args ...interface{}) (interface{}, error)
}

// Rows interface for query results
type Rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close()
}

// Row interface for single row results
type Row interface {
	Scan(dest ...interface{}) error
}

// ConditionalManager interface for managing conditional orders
type ConditionalManager interface {
	AddOrder(order *ConditionalOrder)
}

// ConditionalOrder simplified for receiver
type ConditionalOrder struct {
	ID                string      `json:"id"`
	UserID            string      `json:"user_id"`
	Symbol            string      `json:"symbol"`
	TriggerConditions interface{} `json:"trigger_conditions"`
	Action            interface{} `json:"action"`
	FollowUp          interface{} `json:"follow_up,omitempty"`
	Status            string      `json:"status"`
}

// NewHTTPReceiver creates a new HTTP signal receiver
func NewHTTPReceiver(port int, eventChan chan<- types.Event, logger *slog.Logger) *HTTPReceiver {
	return &HTTPReceiver{
		port:      port,
		eventChan: eventChan,
		logger:    logger,
	}
}

// SetPool sets the database pool for conditional orders
func (r *HTTPReceiver) SetPool(pool Pool) {
	r.pool = pool
}

// SetConditionalManager sets the conditional order manager
func (r *HTTPReceiver) SetConditionalManager(mgr ConditionalManager) {
	r.conditionalMgr = mgr
}

// Start starts the HTTP server
func (r *HTTPReceiver) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Legacy signal endpoint
	mux.HandleFunc("/signal", r.handleSignal)

	// Bot management endpoints (PostgreSQL mode)
	mux.HandleFunc("/bot/start", r.handleBotStart)
	mux.HandleFunc("/bot/stop", r.handleBotStop)
	mux.HandleFunc("/bot/", r.handleBotStatus) // /bot/{id}/status
	mux.HandleFunc("/bots", r.handleBotsList)

	// Conditional order endpoints
	mux.HandleFunc("/conditional", r.handleConditionalOrders)      // POST create, GET list
	mux.HandleFunc("/conditional/", r.handleConditionalOrderByID) // GET/DELETE by ID

	// Health and info endpoints
	mux.HandleFunc("/health", r.handleHealth)
	mux.HandleFunc("/", r.handleRoot)

	r.server = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", r.port),
		Handler:      r.loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	r.logger.Info("[RECEIVER] Starting HTTP server",
		"port", r.port,
		"address", r.server.Addr,
	)

	// Run server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := r.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait briefly to check for immediate errors
	select {
	case err := <-errChan:
		return fmt.Errorf("failed to start server: %w", err)
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// Stop gracefully shuts down the HTTP server
func (r *HTTPReceiver) Stop(ctx context.Context) error {
	if r.server == nil {
		return nil
	}

	r.logger.Info("[RECEIVER] Shutting down HTTP server")
	return r.server.Shutdown(ctx)
}

// loggingMiddleware logs all incoming requests
func (r *HTTPReceiver) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, req)

		r.logger.Info("[RECEIVER] Request",
			"method", req.Method,
			"path", req.URL.Path,
			"status", wrapped.statusCode,
			"duration", time.Since(start),
			"remote", req.RemoteAddr,
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// handleRoot handles requests to the root path
func (r *HTTPReceiver) handleRoot(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" {
		http.NotFound(w, req)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"service": "tradecore",
		"version": "2.0.0",
		"endpoints": []string{
			"POST /signal - Submit trading signal (legacy)",
			"POST /bot/start - Start a bot",
			"POST /bot/stop - Stop a bot",
			"GET /bot/{id}/status - Get bot status",
			"GET /bots?user_id=xxx - List active bots",
			"GET /health - Health check",
		},
	})
}

// handleHealth handles health check requests
func (r *HTTPReceiver) handleHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// handleSignal handles incoming trading signals
func (r *HTTPReceiver) handleSignal(w http.ResponseWriter, req *http.Request) {
	// Only accept POST requests
	if req.Method != http.MethodPost {
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse request body
	var signalReq SignalRequest
	if err := json.NewDecoder(req.Body).Decode(&signalReq); err != nil {
		r.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate request
	if err := r.validateSignal(&signalReq); err != nil {
		r.sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Convert to internal event
	event := types.SignalEvent{
		Symbol:     signalReq.Symbol,
		Action:     types.Action(signalReq.Action),
		GridConfig: signalReq.GridConfig,
		SLConfig:   signalReq.SLConfig,
		Timestamp:  time.Now(),
	}

	// Send to engine via channel
	select {
	case r.eventChan <- types.Event{
		Type:   types.EventTypeSignal,
		Signal: &event,
	}:
		r.logger.Info("[RECEIVER] Signal received",
			"symbol", event.Symbol,
			"action", event.Action,
		)
		r.sendSuccess(w, "Signal received", event)
	case <-time.After(5 * time.Second):
		r.sendError(w, http.StatusServiceUnavailable, "Engine busy, try again")
	}
}

// validateSignal validates the incoming signal request
func (r *HTTPReceiver) validateSignal(req *SignalRequest) error {
	if req.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}

	// Validate action
	validActions := map[string]bool{
		string(types.ActionStartGrid):         true,
		string(types.ActionStopGrid):          true,
		string(types.ActionStartTrailingStop): true,
		string(types.ActionStopTrailingStop):  true,
	}

	if !validActions[req.Action] {
		return fmt.Errorf("invalid action: %s (valid: START_GRID, STOP_GRID, START_TRAILING_STOP, STOP_TRAILING_STOP)", req.Action)
	}

	// Validate grid config if starting grid bot
	if req.Action == string(types.ActionStartGrid) {
		if req.GridConfig == nil {
			return fmt.Errorf("grid_config is required for START_GRID action")
		}
		if err := r.validateGridConfig(req.GridConfig); err != nil {
			return fmt.Errorf("invalid grid_config: %w", err)
		}
	}

	// Validate trailing stop config if starting trailing stop
	if req.Action == string(types.ActionStartTrailingStop) {
		if req.SLConfig == nil {
			return fmt.Errorf("sl_config is required for START_TRAILING_STOP action")
		}
		if err := r.validateTrailingStopConfig(req.SLConfig); err != nil {
			return fmt.Errorf("invalid sl_config: %w", err)
		}
	}

	return nil
}

// validateGridConfig validates grid bot configuration
func (r *HTTPReceiver) validateGridConfig(cfg *types.GridConfig) error {
	if cfg.UpperPrice <= 0 {
		return fmt.Errorf("upper_price must be positive")
	}
	if cfg.LowerPrice <= 0 {
		return fmt.Errorf("lower_price must be positive")
	}
	if cfg.UpperPrice <= cfg.LowerPrice {
		return fmt.Errorf("upper_price must be greater than lower_price")
	}
	if cfg.GridLines < 2 {
		return fmt.Errorf("grid_lines must be at least 2")
	}
	if cfg.GridLines > 100 {
		return fmt.Errorf("grid_lines must not exceed 100")
	}
	if cfg.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	return nil
}

// validateTrailingStopConfig validates trailing stop configuration
func (r *HTTPReceiver) validateTrailingStopConfig(cfg *types.TrailingStopConfig) error {
	if cfg.ActivationPrice < 0 {
		return fmt.Errorf("activation_price must be non-negative")
	}
	if cfg.CallbackRate <= 0 || cfg.CallbackRate >= 1 {
		return fmt.Errorf("callback_rate must be between 0 and 1 (e.g., 0.02 for 2%%)")
	}
	if cfg.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	return nil
}

// sendError sends an error response
func (r *HTTPReceiver) sendError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// sendSuccess sends a success response
func (r *HTTPReceiver) sendSuccess(w http.ResponseWriter, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": message,
		"data":    data,
	})
}

// handleBotStart handles POST /bot/start - start a bot
func (r *HTTPReceiver) handleBotStart(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var startReq types.StartBotRequest
	if err := json.NewDecoder(req.Body).Decode(&startReq); err != nil {
		r.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate request
	if startReq.BotID == "" {
		r.sendError(w, http.StatusBadRequest, "bot_id is required")
		return
	}
	if startReq.UserID == "" {
		r.sendError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if startReq.Symbol == "" {
		r.sendError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if startReq.Type == "" {
		r.sendError(w, http.StatusBadRequest, "type is required")
		return
	}

	// Validate bot type
	validTypes := map[string]bool{
		"grid": true, "dca": true, "rsi": true, "ma_crossover": true,
		"macd": true, "breakout": true, "mean_reversion": true, "momentum": true, "custom": true,
	}
	if !validTypes[startReq.Type] {
		r.sendError(w, http.StatusBadRequest, fmt.Sprintf("invalid type: %s", startReq.Type))
		return
	}

	// Create bot command event
	cmd := &types.BotCommand{
		Command: "start",
		BotID:   startReq.BotID,
		UserID:  startReq.UserID,
		BotType: startReq.Type,
		Symbol:  startReq.Symbol,
		Config:  startReq.Config,
	}

	// Send to engine via channel
	select {
	case r.eventChan <- types.Event{
		Type:       types.EventTypeBotCommand,
		BotCommand: cmd,
	}:
		r.logger.Info("[RECEIVER] Bot start command received",
			"bot_id", startReq.BotID,
			"symbol", startReq.Symbol,
			"type", startReq.Type,
		)
		r.sendSuccess(w, "Bot start command sent", map[string]string{
			"bot_id": startReq.BotID,
			"status": "starting",
		})
	case <-time.After(5 * time.Second):
		r.sendError(w, http.StatusServiceUnavailable, "Engine busy, try again")
	}
}

// handleBotStop handles POST /bot/stop - stop a bot
func (r *HTTPReceiver) handleBotStop(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var stopReq types.StopBotRequest
	if err := json.NewDecoder(req.Body).Decode(&stopReq); err != nil {
		r.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if stopReq.BotID == "" {
		r.sendError(w, http.StatusBadRequest, "bot_id is required")
		return
	}

	// Create bot command event
	cmd := &types.BotCommand{
		Command: "stop",
		BotID:   stopReq.BotID,
	}

	// Send to engine via channel
	select {
	case r.eventChan <- types.Event{
		Type:       types.EventTypeBotCommand,
		BotCommand: cmd,
	}:
		r.logger.Info("[RECEIVER] Bot stop command received", "bot_id", stopReq.BotID)
		r.sendSuccess(w, "Bot stop command sent", map[string]string{
			"bot_id": stopReq.BotID,
			"status": "stopping",
		})
	case <-time.After(5 * time.Second):
		r.sendError(w, http.StatusServiceUnavailable, "Engine busy, try again")
	}
}

// handleBotStatus handles GET /bot/{id}/status - get bot status
func (r *HTTPReceiver) handleBotStatus(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse bot ID from path: /bot/{id}/status or /bot/{id}
	path := req.URL.Path
	if len(path) < 6 { // Minimum: "/bot/x"
		r.sendError(w, http.StatusBadRequest, "Invalid bot ID in path")
		return
	}

	// Remove /bot/ prefix and /status suffix if present
	botID := path[5:] // Remove "/bot/"
	if len(botID) > 7 && botID[len(botID)-7:] == "/status" {
		botID = botID[:len(botID)-7]
	}
	// Remove trailing slash if present
	if len(botID) > 0 && botID[len(botID)-1] == '/' {
		botID = botID[:len(botID)-1]
	}

	if botID == "" {
		r.sendError(w, http.StatusBadRequest, "Bot ID is required")
		return
	}

	// For now, return a placeholder response
	// In production, this would query the engine's active bots
	r.sendSuccess(w, "Bot status", types.BotStatusResponse{
		BotID:  botID,
		Status: "unknown", // Would be filled from engine state
	})
}

// handleBotsList handles GET /bots - list all bots
func (r *HTTPReceiver) handleBotsList(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get optional user_id filter from query params
	userID := req.URL.Query().Get("user_id")

	// For now, return a placeholder response
	// In production, this would query the engine's active bots
	r.sendSuccess(w, "Active bots list", map[string]interface{}{
		"bots":    []interface{}{},
		"user_id": userID,
		"count":   0,
	})
}

// ============================================
// Conditional Order Handlers
// ============================================

// CreateConditionalOrderRequest for API
type CreateConditionalOrderRequest struct {
	UserID            string      `json:"user_id"`
	Symbol            string      `json:"symbol"`
	TriggerConditions interface{} `json:"trigger_conditions"`
	Action            interface{} `json:"action"`
	FollowUp          interface{} `json:"follow_up,omitempty"`
	ExpiresAt         *string     `json:"expires_at,omitempty"`
	Notes             *string     `json:"notes,omitempty"`
	OriginalText      *string     `json:"original_text,omitempty"`
}

// handleConditionalOrders handles POST /conditional (create) and GET /conditional (list)
func (r *HTTPReceiver) handleConditionalOrders(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		r.handleCreateConditionalOrder(w, req)
	case http.MethodGet:
		r.handleListConditionalOrders(w, req)
	default:
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleCreateConditionalOrder creates a new conditional order
func (r *HTTPReceiver) handleCreateConditionalOrder(w http.ResponseWriter, req *http.Request) {
	if r.pool == nil {
		r.sendError(w, http.StatusServiceUnavailable, "Database not configured")
		return
	}

	var createReq CreateConditionalOrderRequest
	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		r.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate required fields
	if createReq.UserID == "" {
		r.sendError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if createReq.Symbol == "" {
		r.sendError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if createReq.TriggerConditions == nil {
		r.sendError(w, http.StatusBadRequest, "trigger_conditions is required")
		return
	}
	if createReq.Action == nil {
		r.sendError(w, http.StatusBadRequest, "action is required")
		return
	}

	// Convert to JSON for storage
	triggerConditionsJSON, _ := json.Marshal(createReq.TriggerConditions)
	actionJSON, _ := json.Marshal(createReq.Action)
	var followUpJSON []byte
	if createReq.FollowUp != nil {
		followUpJSON, _ = json.Marshal(createReq.FollowUp)
	}

	// Insert into database
	ctx := req.Context()
	var orderID string
	row := r.pool.QueryRow(ctx, `
		INSERT INTO conditional_orders (
			user_id, symbol, trigger_conditions, action, follow_up,
			status, expires_at, notes, original_text, source
		) VALUES ($1, $2, $3, $4, $5, 'active', $6, $7, $8, 'api')
		RETURNING id
	`,
		createReq.UserID,
		createReq.Symbol,
		triggerConditionsJSON,
		actionJSON,
		followUpJSON,
		createReq.ExpiresAt,
		createReq.Notes,
		createReq.OriginalText,
	)

	if err := row.Scan(&orderID); err != nil {
		r.logger.Error("[RECEIVER] Failed to create conditional order", "error", err)
		r.sendError(w, http.StatusInternalServerError, "Failed to create order")
		return
	}

	r.logger.Info("[RECEIVER] Conditional order created",
		"order_id", orderID,
		"user_id", createReq.UserID,
		"symbol", createReq.Symbol,
	)

	r.sendSuccess(w, "Conditional order created", map[string]interface{}{
		"order_id": orderID,
		"status":   "active",
	})
}

// handleListConditionalOrders lists conditional orders for a user
func (r *HTTPReceiver) handleListConditionalOrders(w http.ResponseWriter, req *http.Request) {
	if r.pool == nil {
		r.sendError(w, http.StatusServiceUnavailable, "Database not configured")
		return
	}

	userID := req.URL.Query().Get("user_id")
	status := req.URL.Query().Get("status")

	ctx := req.Context()
	query := `
		SELECT id, user_id, symbol, trigger_conditions, action, follow_up,
		       status, created_at, expires_at, notes, original_text
		FROM conditional_orders
		WHERE ($1 = '' OR user_id = $1::uuid)
		  AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT 100
	`

	rows, err := r.pool.Query(ctx, query, userID, status)
	if err != nil {
		r.logger.Error("[RECEIVER] Failed to list conditional orders", "error", err)
		r.sendError(w, http.StatusInternalServerError, "Failed to list orders")
		return
	}
	defer rows.Close()

	orders := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, uid, symbol, orderStatus string
		var triggerConditions, action, followUp []byte
		var createdAt, expiresAt, notes, originalText interface{}

		err := rows.Scan(&id, &uid, &symbol, &triggerConditions, &action, &followUp,
			&orderStatus, &createdAt, &expiresAt, &notes, &originalText)
		if err != nil {
			r.logger.Error("[RECEIVER] Failed to scan order", "error", err)
			continue
		}

		order := map[string]interface{}{
			"id":         id,
			"user_id":    uid,
			"symbol":     symbol,
			"status":     orderStatus,
			"created_at": createdAt,
		}

		// Parse JSON fields
		if len(triggerConditions) > 0 {
			var tc interface{}
			json.Unmarshal(triggerConditions, &tc)
			order["trigger_conditions"] = tc
		}
		if len(action) > 0 {
			var a interface{}
			json.Unmarshal(action, &a)
			order["action"] = a
		}
		if len(followUp) > 0 {
			var fu interface{}
			json.Unmarshal(followUp, &fu)
			order["follow_up"] = fu
		}
		if expiresAt != nil {
			order["expires_at"] = expiresAt
		}
		if notes != nil {
			order["notes"] = notes
		}
		if originalText != nil {
			order["original_text"] = originalText
		}

		orders = append(orders, order)
	}

	r.sendSuccess(w, "Conditional orders", map[string]interface{}{
		"orders": orders,
		"count":  len(orders),
	})
}

// handleConditionalOrderByID handles GET/DELETE /conditional/{id}
func (r *HTTPReceiver) handleConditionalOrderByID(w http.ResponseWriter, req *http.Request) {
	if r.pool == nil {
		r.sendError(w, http.StatusServiceUnavailable, "Database not configured")
		return
	}

	// Parse order ID from path
	path := req.URL.Path
	orderID := strings.TrimPrefix(path, "/conditional/")
	if orderID == "" {
		r.sendError(w, http.StatusBadRequest, "Order ID is required")
		return
	}

	switch req.Method {
	case http.MethodGet:
		r.handleGetConditionalOrder(w, req, orderID)
	case http.MethodDelete:
		r.handleCancelConditionalOrder(w, req, orderID)
	default:
		r.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleGetConditionalOrder gets a specific conditional order
func (r *HTTPReceiver) handleGetConditionalOrder(w http.ResponseWriter, req *http.Request, orderID string) {
	ctx := req.Context()

	var id, userID, symbol, status string
	var triggerConditions, action, followUp, triggerDetails []byte
	var createdAt, triggeredAt, executedAt, expiresAt interface{}
	var notes, originalText, errorMsg interface{}

	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, symbol, trigger_conditions, action, follow_up,
		       status, trigger_details, created_at, triggered_at, executed_at,
		       expires_at, notes, original_text, error_message
		FROM conditional_orders
		WHERE id = $1
	`, orderID)

	err := row.Scan(&id, &userID, &symbol, &triggerConditions, &action, &followUp,
		&status, &triggerDetails, &createdAt, &triggeredAt, &executedAt,
		&expiresAt, &notes, &originalText, &errorMsg)
	if err != nil {
		r.sendError(w, http.StatusNotFound, "Order not found")
		return
	}

	order := map[string]interface{}{
		"id":         id,
		"user_id":    userID,
		"symbol":     symbol,
		"status":     status,
		"created_at": createdAt,
	}

	// Parse JSON fields
	if len(triggerConditions) > 0 {
		var tc interface{}
		json.Unmarshal(triggerConditions, &tc)
		order["trigger_conditions"] = tc
	}
	if len(action) > 0 {
		var a interface{}
		json.Unmarshal(action, &a)
		order["action"] = a
	}
	if len(followUp) > 0 {
		var fu interface{}
		json.Unmarshal(followUp, &fu)
		order["follow_up"] = fu
	}
	if len(triggerDetails) > 0 {
		var td interface{}
		json.Unmarshal(triggerDetails, &td)
		order["trigger_details"] = td
	}
	if triggeredAt != nil {
		order["triggered_at"] = triggeredAt
	}
	if executedAt != nil {
		order["executed_at"] = executedAt
	}
	if expiresAt != nil {
		order["expires_at"] = expiresAt
	}
	if notes != nil {
		order["notes"] = notes
	}
	if originalText != nil {
		order["original_text"] = originalText
	}
	if errorMsg != nil {
		order["error_message"] = errorMsg
	}

	r.sendSuccess(w, "Conditional order", order)
}

// handleCancelConditionalOrder cancels a conditional order
func (r *HTTPReceiver) handleCancelConditionalOrder(w http.ResponseWriter, req *http.Request, orderID string) {
	ctx := req.Context()

	result, err := r.pool.Exec(ctx, `
		UPDATE conditional_orders
		SET status = 'cancelled'
		WHERE id = $1 AND status IN ('active', 'pending', 'monitoring')
	`, orderID)

	if err != nil {
		r.logger.Error("[RECEIVER] Failed to cancel conditional order", "order_id", orderID, "error", err)
		r.sendError(w, http.StatusInternalServerError, "Failed to cancel order")
		return
	}

	// Log the cancellation event
	r.pool.Exec(ctx, `
		INSERT INTO conditional_order_events (order_id, event_type, event_data)
		VALUES ($1, 'cancelled', '{"source": "api"}')
	`, orderID)

	r.logger.Info("[RECEIVER] Conditional order cancelled", "order_id", orderID)
	r.sendSuccess(w, "Order cancelled", map[string]interface{}{
		"order_id": orderID,
		"result":   result,
	})
}
