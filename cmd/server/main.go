package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"tradecore/internal/engine"
	"tradecore/internal/exchange"
	"tradecore/internal/persistence"
	"tradecore/internal/receiver"
)

// Config holds the application configuration
type Config struct {
	APIKey        string
	SecretKey     string
	Port          int
	MockMode      bool
	StateFile     string
	LogLevel      string
	PostgresMode  bool   // Use PostgreSQL instead of file persistence
}

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	// Parse configuration
	cfg := loadConfig()

	// Setup logger
	logger := setupLogger(cfg.LogLevel)

	logger.Info("Starting TradeCore Server",
		"mock_mode", cfg.MockMode,
		"port", cfg.Port,
		"postgres_mode", cfg.PostgresMode,
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize persistence
	var postgresPersistence *persistence.PostgresPersistence
	var filePersistence *engine.StatePersistence
	var err error

	if cfg.PostgresMode {
		logger.Info("Using PostgreSQL persistence mode (multi-user)")
		postgresPersistence, err = persistence.NewPostgresPersistence(ctx, logger)
		if err != nil {
			logger.Error("Failed to initialize PostgreSQL persistence", "error", err)
			os.Exit(1)
		}
		defer postgresPersistence.Close()
	} else {
		logger.Info("Using file persistence mode (legacy)", "state_file", cfg.StateFile)
		filePersistence = engine.NewStatePersistence(cfg.StateFile, logger)
	}

	// Initialize default executor and streamer (for legacy mode or mock mode)
	var defaultExec exchange.Executor
	var streamer exchange.MarketStreamer

	if cfg.MockMode {
		logger.Info("Running in MOCK MODE - no real trades will be executed")
		defaultExec = exchange.NewMockExecutor(logger)
		streamer = exchange.NewMockMarketStreamer(logger)
	} else if !cfg.PostgresMode {
		// Legacy single-user mode with env API keys
		if cfg.APIKey == "" || cfg.SecretKey == "" {
			logger.Error("API_KEY and SECRET_KEY are required for live trading in legacy mode")
			os.Exit(1)
		}
		defaultExec = exchange.NewBinanceClient(cfg.APIKey, cfg.SecretKey, logger)
		streamer = exchange.NewBinanceStreamer(logger)
	} else {
		// PostgreSQL mode - will create executors per-user dynamically
		// Use a shared streamer for all users
		streamer = exchange.NewBinanceStreamer(logger)
		defaultExec = nil // No default executor in multi-user mode
	}

	// Initialize engine based on mode
	var eng *engine.Engine
	if cfg.PostgresMode {
		eng = engine.NewEngineWithPostgres(defaultExec, streamer, postgresPersistence, logger, cfg.MockMode)
	} else {
		eng = engine.NewEngine(defaultExec, streamer, filePersistence, logger)
	}

	// Initialize HTTP receiver
	httpReceiver := receiver.NewHTTPReceiver(cfg.Port, eng.InputChannel(), logger)

	// Start components
	if err := eng.Start(ctx); err != nil {
		logger.Error("Failed to start engine", "error", err)
		os.Exit(1)
	}

	if err := httpReceiver.Start(ctx); err != nil {
		logger.Error("Failed to start HTTP receiver", "error", err)
		os.Exit(1)
	}

	logger.Info("TradeCore Server is running",
		"http_endpoint", "http://127.0.0.1:"+strconv.Itoa(cfg.Port),
	)
	if cfg.PostgresMode {
		logger.Info("Multi-user mode enabled via PostgreSQL")
		logger.Info("Send bot commands to POST /bot/start, POST /bot/stop, GET /bot/{id}/status")
	} else {
		logger.Info("Legacy mode - Send signals to POST /signal")
	}
	logger.Info("Press Ctrl+C to stop")

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("Received shutdown signal", "signal", sig)

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Stop HTTP receiver first
	if err := httpReceiver.Stop(shutdownCtx); err != nil {
		logger.Error("Error stopping HTTP receiver", "error", err)
	}

	// Stop engine (this will also save state)
	eng.Stop()

	// Close streamer
	if err := streamer.Close(); err != nil {
		logger.Error("Error closing streamer", "error", err)
	}

	// Close executor if exists
	if defaultExec != nil {
		if err := defaultExec.Close(); err != nil {
			logger.Error("Error closing executor", "error", err)
		}
	}

	logger.Info("TradeCore Server stopped gracefully")
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	port := 9090 // Default to 9090 as per plan
	if p := os.Getenv("PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	mockMode := true // Default to mock mode for safety
	if m := os.Getenv("MOCK_MODE"); m != "" {
		mockMode = m == "true" || m == "1" || m == "yes"
	}

	// PostgreSQL mode is enabled if POSTGRES_HOST is set
	postgresMode := os.Getenv("POSTGRES_HOST") != ""

	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		stateFile = "./state.json"
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	return Config{
		APIKey:       os.Getenv("API_KEY"),
		SecretKey:    os.Getenv("SECRET_KEY"),
		Port:         port,
		MockMode:     mockMode,
		StateFile:    stateFile,
		LogLevel:     logLevel,
		PostgresMode: postgresMode,
	}
}

// setupLogger configures the structured logger
func setupLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}
