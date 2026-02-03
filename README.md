# TradeCore

A high-performance, concurrent trading server in Go that implements Grid Bots and Trailing Stop Loss strategies using real-time market data from Binance.

## Architecture

TradeCore uses the **Fan-In Pattern** to multiplex all inputs (HTTP signals, WebSocket price updates) into a single logic engine, ensuring thread-safe state management without complex locking.

```
┌─────────────────┐     ┌─────────────────┐
│  HTTP Receiver  │     │ Binance WebSocket│
│  (Goroutine 1)  │     │  (Goroutine 2)   │
└────────┬────────┘     └────────┬─────────┘
         │                       │
         │    SignalEvent        │   PriceUpdate
         │                       │
         └───────────┬───────────┘
                     │
                     ▼
              ┌──────────────┐
              │  inputChan   │
              │  (buffered)  │
              └──────┬───────┘
                     │
                     ▼
              ┌──────────────┐
              │    Engine    │
              │ (Main Loop)  │
              │              │
              │ for/select { │
              │   case event │
              │   ...        │
              │ }            │
              └──────┬───────┘
                     │
                     ▼
              ┌──────────────┐
              │  ActiveBots  │
              │   (state)    │
              └──────────────┘
```

## Features

- **Grid Bot Trading**: Automatically buy low and sell high within a price range
- **Trailing Stop Loss**: Protect profits by trailing the highest price
- **Real-time Market Data**: WebSocket connection to Binance for live prices
- **Dynamic Subscriptions**: Subscribe/unsubscribe to symbols as bots start/stop
- **Auto-Reconnection**: WebSocket reconnects with exponential backoff
- **State Persistence**: Bot state saved to JSON for crash recovery
- **Mock Mode**: Test strategies without risking real funds
- **Graceful Shutdown**: Clean shutdown with state preservation

## Project Structure

```
tradecore/
├── cmd/
│   └── server/
│       └── main.go              # Entry point, config, graceful shutdown
├── internal/
│   ├── types/
│   │   └── types.go             # Shared structs and constants
│   ├── exchange/
│   │   ├── interface.go         # Executor and MarketStreamer interfaces
│   │   ├── binance.go           # Binance REST and WebSocket implementation
│   │   └── mock.go              # Mock implementation for testing
│   ├── receiver/
│   │   └── http.go              # HTTP server for receiving signals
│   └── engine/
│       ├── engine.go            # Core trading logic engine
│       └── persistence.go       # State persistence to JSON
├── go.mod
├── go.sum
├── .env.example
└── README.md
```

## Installation

### Prerequisites

- Go 1.22 or later
- Binance API credentials (for live trading)

### Build

```bash
# Clone or navigate to the project
cd /opt/tradecore

# Download dependencies
go mod tidy

# Build the binary
go build -o tradecore ./cmd/server/
```

### System User (Recommended)

For production, run as a dedicated system user:

```bash
# Create system user
sudo useradd -r -s /usr/sbin/nologin -d /opt/tradecore -M tradecore

# Set ownership
sudo chown -R tradecore:tradecore /opt/tradecore
```

## Configuration

Copy the example configuration and edit:

```bash
cp .env.example .env
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_KEY` | - | Binance API key (required for live trading) |
| `SECRET_KEY` | - | Binance API secret (required for live trading) |
| `PORT` | `8080` | HTTP server port |
| `MOCK_MODE` | `true` | Enable mock mode (no real trades) |
| `STATE_FILE` | `./state.json` | Path for state persistence |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |

### Example .env

```bash
# For live trading
API_KEY=your_binance_api_key
SECRET_KEY=your_binance_secret_key
PORT=8080
MOCK_MODE=false
STATE_FILE=./state.json
LOG_LEVEL=info
```

## Running

### Mock Mode (Safe Testing)

```bash
# Default runs in mock mode
./tradecore

# Or explicitly
MOCK_MODE=true ./tradecore
```

### Live Trading

```bash
MOCK_MODE=false ./tradecore
```

### As System User

```bash
sudo -u tradecore ./tradecore
```

### With Custom Port

```bash
PORT=9090 ./tradecore
```

## API Reference

### Base URL

```
http://127.0.0.1:8080
```

The server binds to localhost only for security.

---

### Health Check

Check if the server is running.

**Endpoint:** `GET /health`

**Response:**
```json
{
  "status": "healthy",
  "time": "2025-12-16T19:09:46+01:00"
}
```

---

### Submit Signal

Submit a trading signal to start or stop bots.

**Endpoint:** `POST /signal`

**Headers:**
```
Content-Type: application/json
```

**Actions:**
- `START_GRID` - Start a grid bot
- `STOP_GRID` - Stop a grid bot
- `START_TRAILING_STOP` - Start a trailing stop
- `STOP_TRAILING_STOP` - Stop a trailing stop

---

### Start Grid Bot

**Request:**
```json
{
  "symbol": "BTCUSDT",
  "action": "START_GRID",
  "grid_config": {
    "upper_price": 70000,
    "lower_price": 60000,
    "grid_lines": 10,
    "quantity": 0.001
  }
}
```

**Parameters:**

| Field | Type | Description |
|-------|------|-------------|
| `symbol` | string | Trading pair (e.g., BTCUSDT, ETHUSDT) |
| `upper_price` | float | Upper bound of the grid |
| `lower_price` | float | Lower bound of the grid |
| `grid_lines` | int | Number of grid levels (2-100) |
| `quantity` | float | Trade quantity per grid level |

**Response:**
```json
{
  "success": true,
  "message": "Signal received",
  "data": {
    "symbol": "BTCUSDT",
    "action": "START_GRID",
    "grid_config": {
      "upper_price": 70000,
      "lower_price": 60000,
      "grid_lines": 10,
      "quantity": 0.001
    },
    "timestamp": "2025-12-16T19:09:46.266152117+01:00"
  }
}
```

---

### Stop Grid Bot

**Request:**
```json
{
  "symbol": "BTCUSDT",
  "action": "STOP_GRID"
}
```

---

### Start Trailing Stop

**Request:**
```json
{
  "symbol": "ETHUSDT",
  "action": "START_TRAILING_STOP",
  "sl_config": {
    "activation_price": 3600,
    "callback_rate": 0.02,
    "quantity": 1.0
  }
}
```

**Parameters:**

| Field | Type | Description |
|-------|------|-------------|
| `symbol` | string | Trading pair |
| `activation_price` | float | Price at which trailing activates (0 = immediate) |
| `callback_rate` | float | Callback percentage as decimal (0.02 = 2%) |
| `quantity` | float | Quantity to sell when triggered |

---

### Stop Trailing Stop

**Request:**
```json
{
  "symbol": "ETHUSDT",
  "action": "STOP_TRAILING_STOP"
}
```

---

### Error Responses

**Invalid JSON:**
```json
{
  "success": false,
  "error": "Invalid JSON: unexpected EOF"
}
```

**Validation Error:**
```json
{
  "success": false,
  "error": "invalid grid_config: upper_price must be greater than lower_price"
}
```

## Trading Logic

### Grid Bot

The Grid Bot divides a price range into equal levels and trades when price crosses each level.

**Setup:**
1. Calculate grid step: `GridStep = (UpperPrice - LowerPrice) / GridLines`
2. Create levels from `LowerPrice` to `UpperPrice`

**Execution:**
- When price crosses **down** through a level: Execute **BUY**
- When price crosses **up** through a filled level: Execute **SELL**

**Example:**
```
Upper: 70,000
Lower: 60,000
Grid Lines: 10
Grid Step: 1,000

Levels: 60000, 61000, 62000, ..., 70000

Price drops from 65500 to 64800:
  → BUY triggered at 65000 level

Price rises from 64800 to 65200:
  → SELL triggered at 65000 level (was filled)
```

### Trailing Stop

The Trailing Stop tracks the highest price and triggers a sell when price drops by the callback rate.

**Formula:**
```
StopPrice = HighestPrice * (1 - CallbackRate)
```

**Example:**
```
Activation Price: 3600
Callback Rate: 0.02 (2%)

Price reaches 3600 → Trailing activated
Price rises to 3800 → HighestPrice = 3800
StopPrice = 3800 * (1 - 0.02) = 3724

Price drops to 3720 → SELL triggered (below 3724)
```

## Logs

TradeCore uses structured logging with the following prefixes:

| Prefix | Component |
|--------|-----------|
| `[ENGINE]` | Core trading engine |
| `[RECEIVER]` | HTTP signal receiver |
| `[BINANCE]` | Binance adapter |
| `[MOCK]` | Mock executor/streamer |
| `[PERSISTENCE]` | State persistence |

**Example Output:**
```
level=INFO msg="[ENGINE] Grid bot started" symbol=BTCUSDT upper=70000 lower=60000 grid_lines=10 grid_step=1000 quantity=0.001
level=INFO msg="[ENGINE] Grid Buy Filled" symbol=BTCUSDT level=5 price=65000 quantity=0.001 order_id=MOCK-1
level=INFO msg="[ENGINE] Trailing Stop Triggered" symbol=ETHUSDT highest_price=3800 stop_price=3724 executed_price=3720 quantity=1
```

## State Persistence

Bot state is automatically saved to `STATE_FILE` (default: `./state.json`):

- **Periodic saves**: Every 30 seconds when state changes
- **Shutdown save**: Final save on graceful shutdown
- **Atomic writes**: Uses temp file + rename for safety
- **Recovery**: State restored on startup

**State File Format:**
```json
{
  "active_bots": {
    "BTCUSDT": {
      "symbol": "BTCUSDT",
      "grid": {
        "active": true,
        "upper_price": 70000,
        "lower_price": 60000,
        "grid_step": 1000,
        "levels": [...],
        "last_price": 65432.10
      },
      "created_at": "2025-12-16T19:09:46Z",
      "updated_at": "2025-12-16T19:15:30Z"
    }
  },
  "saved_at": "2025-12-16T19:15:30Z",
  "version": 1
}
```

## Examples

### cURL Examples

```bash
# Health check
curl http://127.0.0.1:8080/health

# Start grid bot for BTCUSDT
curl -X POST http://127.0.0.1:8080/signal \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "BTCUSDT",
    "action": "START_GRID",
    "grid_config": {
      "upper_price": 70000,
      "lower_price": 60000,
      "grid_lines": 10,
      "quantity": 0.001
    }
  }'

# Start trailing stop for ETHUSDT
curl -X POST http://127.0.0.1:8080/signal \
  -H "Content-Type: application/json" \
  -d '{
    "symbol": "ETHUSDT",
    "action": "START_TRAILING_STOP",
    "sl_config": {
      "activation_price": 0,
      "callback_rate": 0.03,
      "quantity": 0.5
    }
  }'

# Stop grid bot
curl -X POST http://127.0.0.1:8080/signal \
  -H "Content-Type: application/json" \
  -d '{"symbol": "BTCUSDT", "action": "STOP_GRID"}'

# Stop trailing stop
curl -X POST http://127.0.0.1:8080/signal \
  -H "Content-Type: application/json" \
  -d '{"symbol": "ETHUSDT", "action": "STOP_TRAILING_STOP"}'
```

### Python Example

```python
import requests

BASE_URL = "http://127.0.0.1:8080"

# Start a grid bot
response = requests.post(f"{BASE_URL}/signal", json={
    "symbol": "BTCUSDT",
    "action": "START_GRID",
    "grid_config": {
        "upper_price": 70000,
        "lower_price": 60000,
        "grid_lines": 10,
        "quantity": 0.001
    }
})
print(response.json())

# Start trailing stop
response = requests.post(f"{BASE_URL}/signal", json={
    "symbol": "ETHUSDT",
    "action": "START_TRAILING_STOP",
    "sl_config": {
        "activation_price": 3500,
        "callback_rate": 0.02,
        "quantity": 1.0
    }
})
print(response.json())
```

## Systemd Service (Production)

Create `/etc/systemd/system/tradecore.service`:

```ini
[Unit]
Description=TradeCore Trading Server
After=network.target

[Service]
Type=simple
User=tradecore
Group=tradecore
WorkingDirectory=/opt/tradecore
ExecStart=/opt/tradecore/tradecore
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

# Environment
Environment=MOCK_MODE=true
Environment=PORT=8080
Environment=LOG_LEVEL=info

# Security
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/tradecore

[Install]
WantedBy=multi-user.target
```

**Enable and start:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable tradecore
sudo systemctl start tradecore
sudo systemctl status tradecore

# View logs
sudo journalctl -u tradecore -f
```

## Security Considerations

1. **Localhost Only**: HTTP server binds to `127.0.0.1` only
2. **Mock Mode Default**: Prevents accidental live trading
3. **No Login User**: System user has no shell access
4. **API Key Protection**: Store keys in `.env` with restricted permissions
5. **State File**: Contains trading state, protect with file permissions

```bash
# Secure the .env file
chmod 600 /opt/tradecore/.env

# Secure the state file
chmod 600 /opt/tradecore/state.json
```

## Troubleshooting

### Port Already in Use

```bash
# Find process using the port
lsof -i :8080

# Use a different port
PORT=9090 ./tradecore
```

### WebSocket Disconnections

The server automatically reconnects with exponential backoff. Check logs for:
```
level=WARN msg="[BINANCE] WebSocket disconnected, reconnecting..."
```

### State Recovery Issues

If state file is corrupted:
```bash
# Backup and remove
mv state.json state.json.bak
# Server will start fresh
./tradecore
```

### Mock Mode Not Working

Ensure environment variable is set correctly:
```bash
# Check current setting
echo $MOCK_MODE

# Force mock mode
MOCK_MODE=true ./tradecore
```

## Dependencies

- [go-binance](https://github.com/adshao/go-binance) - Binance API client
- [godotenv](https://github.com/joho/godotenv) - Environment file loading

## License

MIT License
