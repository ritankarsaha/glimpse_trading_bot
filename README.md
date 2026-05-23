# Glimpse Bot

A local trading bot for the [Glimpse](https://www.glimpse.markets) Bitcoin prediction market platform. Runs as a Go backend on `localhost:8080` with a Next.js dashboard on `localhost:3000`, and connects to the Glimpse API to place trades — or simulate them in dry-run mode.

---

## Quick Start

**1. Start the Go backend**

```bash
cd glimpse-bot
go run ./...
```

**2. Start the Next.js frontend** (in a separate terminal)

```bash
cd glimpse-bot/frontend
npm install        
npm run dev
```

Open your browser at **http://localhost:3000**.

> **Dry run is ON by default.** No real trades are placed until you uncheck the "Dry run" box and click Start.

---

## Prerequisites

- Go 1.21 or later (`go version`)
- Node.js 18 or later + npm (`node -v`)
- A Glimpse account with a funded Lightning wallet
- A valid Glimpse JWT (see below)

---

## Configuration

Settings can be provided three ways (highest priority wins):

| Source | How |
|--------|-----|
| Environment variables | `export GLIMPSE_TOKEN=eyJ…` |
| `config.json` in working directory | see example below |
| UI at runtime | paste token, adjust sliders, click Start |

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GLIMPSE_TOKEN` | _(empty)_ | JWT auth token |
| `GLIMPSE_TOPIC_ID` | `1116` | Market topic to trade |
| `GLIMPSE_CONTRACTS` | `1` | Contracts per trade leg |
| `GLIMPSE_STRATEGY` | `nearest` | `nearest` / `feargreed` / `bestodds` / `followcrowd` |
| `GLIMPSE_POLL_SEC` | `60` | Seconds between ticks |
| `GLIMPSE_MAX_TRADES` | `5` | Max trades per session |
| `GLIMPSE_DRY_RUN` | `true` | `true` = log only, no real trades |
| `PORT` | `8080` | HTTP server port |

### config.json example

```json
{
  "topic_id": 1116,
  "contracts": 1,
  "strategy": "nearest",
  "poll_sec": 60,
  "max_trades": 5,
  "dry_run": true,
  "min_expiry_mins": 10,
  "max_wallet_pct": 20,
  "no_duplicates": true,
  "forced_option_id": 0
}
```

---

## Strategies

| Name | Description |
|------|-------------|
| `nearest` | Picks the price range that contains (or is closest to) the live BTC spot price. Safe and mechanical — good default. |
| `feargreed` | Contrarian. When the market is fearful it bets slightly bullish (spot × 1.5–0.75%); when greedy, slightly bearish. Conviction scales with the index value. |
| `bestodds` | Among the 3 ranges nearest to spot, picks the one with the lowest `yes_price` (best return per sat wagered) that still has at least 8% implied probability. Avoids the over-priced "obvious" range. |
| `followcrowd` | Picks whichever outcome has the highest market-implied probability (`yes_price`). Follow-the-crowd momentum — lower return but highest win probability. |

---
## API Endpoints (for debugging)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/ping` | Health check — returns `{"ok": true}` |
| `GET` | `/api/status` | Bot state (running, trades, balances, recent logs) |
| `GET` | `/api/trades` | Full trade history for this session |
| `GET` | `/api/markets` | Active markets sorted by soonest expiry |
| `GET` | `/api/quotes?topic_id=N` | Live outcome quotes for a given market |
| `POST` | `/api/start` | Start the bot with config JSON body |
| `POST` | `/api/stop` | Stop the bot |
| `POST` | `/api/token` | Update JWT: `{"token": "eyJ…"}` |
| `POST` | `/api/quicktrade` | Place a single one-off trade |
| `GET` | `/api/wallet` | Live wallet balance from Glimpse |
| `GET` | `/api/spot` | Live BTC/USD spot price from Coinbase |

Test the server is up:

```bash
curl http://localhost:8080/api/ping
# {"ok":true}
```

---

## Project Structure

```
glimpse-bot/
├── main.go                  HTTP server entry point, graceful shutdown
├── config/config.go         Config loading (env → config.json → defaults)
├── api/
│   ├── types.go             All Glimpse / Coinbase / F&G API structs
│   └── client.go            HTTP client with retry, auth headers, market filtering
├── strategy/strategy.go     Strategy interface + 4 implementations + RankByProximity
├── bot/bot.go               Tick loop, state machine, NoDuplicates fallback, trade execution
├── web/
│   ├── handlers.go          HTTP route handlers, CORS, embed
dashboard (embedded in binary)
├── frontend/                Next.js 14 dashboard (primary UI)
│   ├── app/
│   │   ├── page.tsx         Main dashboard page
│   │   └── layout.tsx       Root layout with global styles
│   ├── components/
│   │   ├── ConfigCard.tsx   Bot config form, range picker, quick trade
│   │   ├── StatsGrid.tsx    Live stats cards (balance, spot, F&G, trades)
│   │   ├── TradeTable.tsx   Session trade history table
│   │   └── LogPanel.tsx     Scrolling bot log viewer
│   ├── lib/
│   │   ├── api.ts           Typed fetch wrappers for all backend endpoints
│   │   └── types.ts         Shared TypeScript interfaces
│   ├── next.config.mjs      Proxy: /api/* → localhost:8080
│   └── package.json
├── go.mod
└── README.md
```

---

## Sample Backend Logs

```
2026/05/24 20:45:50 [API] GET https://api.coinbase.com/v2/prices/BTC-USD/spot (attempt 1/3)
2026/05/24 20:45:51 [API] GET https://api.coinbase.com/v2/prices/BTC-USD/spot → 200
2026/05/24 20:45:51 [API] GET https://api.alternative.me/fng/?limit=1 (attempt 1/3)
2026/05/24 20:45:51 [API] GET https://api.alternative.me/fng/?limit=1 → 200
2026/05/24 20:45:51 [API] GET https://glimpesdev.bpmapi.io/api/v1/wallet-balance (attempt 1/3)
2026/05/24 20:45:51 [API] GET https://glimpesdev.bpmapi.io/api/v1/wallet-balance → 200
2026/05/24 20:45:51 [API] GET https://glimpesdev.bpmapi.io/api/v1/nmarket/markets/1164/quotes (attempt 1/3)
2026/05/24 20:45:52 [API] GET https://glimpesdev.bpmapi.io/api/v1/nmarket/markets/1164/quotes → 200
2026/05/24 20:45:52 [API] POST https://glimpesdev.bpmapi.io/api/v1/nmarket/enter-multi-topic-multi-leg (attempt 1/3)
2026/05/24 20:45:53 [API] POST https://glimpesdev.bpmapi.io/api/v1/nmarket/enter-multi-topic-multi-leg → 200
```

## Sample Bot Logs — NoDuplicates Fallback in Action

```
[20:45:16] Bot started — strategy=feargreed topicID=1164 contracts=1 dryRun=false minExpiry=10m maxWalletPct=10% noDuplicates=true
[20:45:20] tick: spot=$76410.40 fng=25(Extreme Fear) expiry=5h44m40s → option 260 range=76800-77000 yesPrice=1000ms
[20:45:20] Trade placed: id=b157b3e6-a445-4308-905c-51e69f5b4c51 range=76800-77000 cost=0sats comm=1sats
[20:45:32] Option 260 (range=76800-77000) already traded — falling back to option 258 (range=76400-76600)
[20:45:32] tick: spot=$76422.07 fng=25(Extreme Fear) expiry=5h44m27s → option 258 range=76400-76600 yesPrice=32000ms
[20:45:33] Trade placed: id=128dcb0d-45fd-4117-8806-9ec9b62c4e6a range=76400-76600 cost=32sats comm=1sats
[20:45:42] tick: spot=$76436.10 fng=25(Extreme Fear) expiry=5h44m18s → option 261 range=77000-77200 yesPrice=5000ms
[20:45:42] Trade placed: id=5f0452f3-91fc-4e29-8807-b840e22f9f21 range=77000-77200 cost=5sats comm=1sats
[20:45:52] Option 260 (range=76800-77000) already traded — falling back to option 257 (range=76200-76400)
[20:45:52] tick: spot=$76416.01 fng=25(Extreme Fear) expiry=5h44m7s → option 257 range=76200-76400 yesPrice=31000ms
[20:45:53] Trade placed: id=b2267fc6-c704-4037-8fda-f90c0a68ee34 range=76200-76400 cost=31sats comm=1sats
[20:46:02] Option 260 (range=76800-77000) already traded — falling back to option 259 (range=76600-76800)
[20:46:02] tick: spot=$76416.01 fng=25(Extreme Fear) expiry=5h43m57s → option 259 range=76600-76800 yesPrice=9000ms
[20:46:03] Trade placed: id=ecd8481c-9aa1-41f6-a453-250f5aff9e6b range=76600-76800 cost=9sats comm=1sats
[20:46:03] Max trades (5) reached — stopping
[20:46:03] Bot stopped
```

---

## How the Interface Looks

<img width="1499" height="802" alt="Screenshot 2026-05-25 at 2 11 05 AM" src="https://github.com/user-attachments/assets/c11525c1-2d7e-452a-859f-6bf49aafa81e" />

## Other Related Screenshots

<img width="1501" height="679" alt="Screenshot 2026-05-25 at 2 08 30 AM" src="https://github.com/user-attachments/assets/76e0dd45-cff4-4d5b-8c2a-cbc9176d3e85" />

Bot mostly comes with a positive trade response

<img width="621" height="446" alt="Screenshot 2026-05-25 at 2 09 31 AM" src="https://github.com/user-attachments/assets/ee455c2e-8291-4d80-9bea-d92e7a1c3f2b" />
<img width="503" height="433" alt="Screenshot 2026-05-25 at 2 09 18 AM" src="https://github.com/user-attachments/assets/4398c08d-0d4d-43c4-a1db-df757d974467" />
<img width="525" height="425" alt="Screenshot 2026-05-25 at 2 32 07 AM" src="https://github.com/user-attachments/assets/0e814df3-1416-4e32-8799-2eb597e29279" />
