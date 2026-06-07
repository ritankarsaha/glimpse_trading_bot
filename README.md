# Glimpse Bot

A local trading bot for the [Glimpse](https://www.glimpse.markets) Bitcoin prediction market platform. Runs as a Go backend on `localhost:3002` with a Next.js dashboard on `localhost:3000` (or a Bloomberg-style terminal UI), and connects to the Glimpse API to place trades — or simulate them in dry-run mode.

It supports two trading modes:

- **Strategy mode** — simple, mechanical heuristics (`nearest`, `feargreed`, `bestodds`, `followcrowd`) that pick a single price-range option per tick.
- **Kelly mode** — a full quantitative pipeline: a pluggable forecaster estimates the probability distribution of BTC's price at expiry, compares it against the live LMSR market-implied distribution, and a fractional-Kelly optimizer sizes a multi-bin portfolio that maximizes expected log-wealth (per Benter's 1994 handicapping framework, adapted to Glimpse's QLS-LMSR mechanism — see [`docs/`](#research--references)).

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

**Or skip the web UI entirely and run the terminal dashboard:**

```bash
go run . -tui
```

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
| `GLIMPSE_CONTRACTS` | `1` | Contracts per trade leg (strategy mode) |
| `GLIMPSE_STRATEGY` | `nearest` | `nearest` / `feargreed` / `bestodds` / `followcrowd` |
| `GLIMPSE_POLL_SEC` | `60` | Seconds between ticks |
| `GLIMPSE_MAX_TRADES` | `5` | Max trades per session |
| `GLIMPSE_DRY_RUN` | `true` | `true` = log only, no real trades |
| `GLIMPSE_MIN_EXPIRY_MINS` | `10` | Stop trading once a market is within this many minutes of expiry |
| `GLIMPSE_MAX_WALLET_PCT` | `10` | Max % of wallet balance to risk per session |
| `GLIMPSE_NO_DUPLICATES` | `true` | `true` = don't re-trade an already-traded option; fall back to the next-best one |
| `GLIMPSE_MODEL` | `gaussian` | Forecaster: `gaussian` / `skewed` / `uniform` / `lognormal`, or a JSON file path / HTTP URL |
| `GLIMPSE_MODEL_URL` | _(empty)_ | HTTP forecaster endpoint; overrides `GLIMPSE_MODEL` when set |
| `GLIMPSE_ANNUAL_VOL_PCT` | `65.0` | Annualised BTC volatility in %, used by the forecaster models |
| `GLIMPSE_KELLY_FRACTION` | `0.25` | Fractional Kelly multiplier; `0` disables Kelly mode and falls back to strategy mode |
| `GLIMPSE_MIN_EDGE` | `0.005` | Minimum (model − market) probability edge required to trade a bin |
| `GLIMPSE_MAX_BINS` | `10` | Max number of mispriced bins to trade per Kelly round |
| `PORT` | `3002` | HTTP server port |

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

  "model": "lognormal",
  "annual_vol_pct": 65.0,
  "kelly_fraction": 0.25,
  "min_edge": 0.005,
  "max_bins": 10
}
```

---

## Trading Modes

The bot picks its mode based on `kelly_fraction`:

- **`kelly_fraction == 0`** → **Strategy mode**. Each tick, the configured strategy picks a single best option and places one trade for `contracts` contracts (with `NoDuplicates` fallback to the next-best option if the top pick was already traded).
- **`kelly_fraction > 0`** → **Kelly mode**. Each tick, the bot:
  1. Builds the live LMSR quantity vector from the market's outcomes.
  2. Asks the configured forecaster for a full probability distribution over all 500 price bins.
  3. Runs the Kelly optimizer to find which bins are mispriced (model probability exceeds market-implied probability by more than `min_edge`) and how many contracts to buy in each, subject to a `kelly_fraction × wallet` budget.
  4. Executes (or, in dry-run, logs) each recommended trade and tracks it as a `KellyTradeSummary` (bin range, market %, model %, edge %, contracts, cost).

### Strategies (strategy mode)

| Name | Description |
|------|-------------|
| `nearest` | Picks the price range that contains (or is closest to) the live BTC spot price. Safe and mechanical — good default. |
| `feargreed` | Contrarian. When the market is fearful it bets slightly bullish (spot × 1.5–0.75%); when greedy, slightly bearish. Conviction scales with the index value. |
| `bestodds` | Among the 3 ranges nearest to spot, picks the one with the lowest `yes_price` (best return per sat wagered) that still has at least 8% implied probability. Avoids the over-priced "obvious" range. |
| `followcrowd` | Picks whichever outcome has the highest market-implied probability (`yes_price`). Follow-the-crowd momentum — lower return but highest win probability. |

### Forecast models (Kelly mode)

Selected via `model` / `GLIMPSE_MODEL` (or `model_url` to use an HTTP forecaster), passed to `model.FromName(name, sigmaPct)`:

| Model | Description |
|-------|-------------|
| `gaussian` | Normal distribution centered on spot. σ = `spot × sigma_pct / 100`, or `spot × annual_vol × √(time-to-expiry / 1yr)` when volatility + expiry are available. |
| `skewed` | Gaussian whose mean is shifted by the Fear & Greed index (contrarian skew of up to ±2% of spot — fearful markets skew the forecast bullish and vice versa). |
| `uniform` | Equal probability across all 500 bins — a neutral baseline / sanity check. |
| `lognormal` | `log(P_T) ~ N(log(S₀) − ½σ²T, σ²T)`. Right-skewed and bounded at zero, the theoretically correct model for asset prices; uses `annual_vol_pct` and time-to-expiry. |
| `<https://...>` | **HTTP forecaster** — POSTs a `ForecastContext` JSON (spot price, Fear & Greed index, time-to-expiry, live market-implied probabilities, annualised vol) to the URL and expects a `[]float64` of 500 probabilities back. Lets you plug in your own model server. |
| `<path/to/file.json>` | **Static JSON model** — reads a fixed probability array from a local file. |

### Kelly optimizer

`kelly.DefaultConfig()`:

| Field | Default | Meaning |
|-------|---------|---------|
| `KellyFraction` | `0.25` | Quarter-Kelly — Benter's recommended ceiling for a new/uncertain model |
| `Tau` | `0.02` | Assumed fee/slippage (matches the LMSR mechanism's `Tau`) |
| `MinEdge` | `0.005` | Only trade bins where `model_prob − market_prob` exceeds this |
| `MaxBins` | `10` | Cap on mispriced bins traded per round |
| `MaxIterations` | `300` | Iterations of the projected-gradient solver |

High level algorithm (`kelly.Optimize`):
1. **Screen** — find underpriced bins (edge ≥ `MinEdge`), sort by edge, keep the top `MaxBins`.
2. **Cap** — for each bin, compute the "truthful cap" (the most shares you can buy before the LMSR price rises to your model's estimate) via `lmsr.TruthfulCap`.
3. **Optimize** — projected-gradient ascent on `E[log(wealth)]` subject to total cost ≤ budget (`kelly_fraction × wallet`).
4. **Execute** — scale the optimum by `KellyFraction`, floor to whole contracts, and return the trade list.

### LMSR engine (`lmsr/`)

A constant-product Logarithmic Market Scoring Rule automated market maker over 500 BTC price bins ($200 wide each, covering $0–$100,000+):

- `CostFunction(q)` — `C(q) = 100 · b · ln(Σ exp(qᵢ/b))`, the cost to move the market to state `q` (with `b = α · Σqⱼ`)
- `PriceVector(q)` / `ImpliedProbs(q)` — the marginal price (∂C/∂qᵢ) of each bin, i.e. the market-implied probability distribution
- `TruthfulCap(q, idx, targetProb)` — binary search for the max position size in a bin before its price reaches a target probability (used by the Kelly optimizer's caps)
- `SpotToBin`, `BinLow`/`BinHigh`/`BinMid` — helpers for mapping spot price ⇄ bin index/boundaries

---

## Terminal UI (TUI)

A Bloomberg-style live dashboard that runs entirely in the terminal — useful for headless servers or for watching the Kelly pipeline in detail without a browser.

```bash
go run . -tui [-model gaussian] [-sigma 5.0] [-kelly 0.25] [-annual-vol 65.0]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-tui` | `false` | Launch the terminal UI instead of the web server |
| `-model` | _(config default)_ | Forecast model: `gaussian`, `skewed`, `uniform`, `lognormal`, or a JSON/HTTP path |
| `-sigma` | `5.0` | Gaussian σ as % of spot price (used by `gaussian`/`skewed` when vol+time aren't available) |
| `-kelly` | _(config default)_ | Kelly fraction `c` (`0 < c ≤ 1`; `0.25` = quarter-Kelly) |
| `-annual-vol` | _(config default)_ | BTC annualised volatility in % (e.g. `65.0`) |

It displays:
- A header with live BTC spot price, Fear & Greed index, time-to-expiry, and LIVE/PAUSED + dry-run status
- A 17-bin histogram (±8 bins around spot) overlaying the market-implied distribution (▓) with the model's forecast (▒, highlighted in cyan), flagging mispriced bins with edges over 0.5%
- A live Kelly trade panel (bin range, market %, model %, edge %, contracts, cost in sats)
- A scrolling log of the last session trades and tick events

Key bindings: `q` quit · `s` start/stop · `d` toggle dry-run · `r` force refresh.

---

## API Endpoints (for debugging)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/ping` | Health check — returns `{"ok": true}` |
| `GET` | `/api/status` | Bot state (running, trades, balances, model/Kelly fields, recent logs) |
| `GET` | `/api/trades` | Full trade history for this session |
| `GET` | `/api/markets` | Active markets sorted by soonest expiry |
| `GET` | `/api/quotes?topic_id=N` | Live outcome quotes for a given market |
| `POST` | `/api/start` | Start the bot with config JSON body (strategy or Kelly mode) |
| `POST` | `/api/stop` | Stop the bot |
| `POST` | `/api/token` | Update JWT: `{"token": "eyJ…"}` |
| `POST` | `/api/quicktrade` | Place a single one-off trade on a specific option, outside of a session |
| `GET` | `/api/wallet` | Live wallet balance from Glimpse |
| `GET` | `/api/spot` | Live BTC/USD spot price from Coinbase |

Test the server is up:

```bash
curl http://localhost:3002/api/ping
# {"ok":true}
```

Start the bot in Kelly mode with a log-normal forecaster (dry run):

```bash
curl -X POST http://localhost:3002/api/start \
  -H 'Content-Type: application/json' \
  -d '{"model":"lognormal","annual_vol_pct":65,"kelly_fraction":0.25,"dry_run":true}'
```

---

## Project Structure

```
glimpse-bot/
├── main.go                  Entry point — web server or -tui mode, graceful shutdown
├── config/config.go         Config loading (env → config.json → defaults)
├── api/
│   ├── types.go             Glimpse / Coinbase / Fear & Greed API structs
│   └── client.go            HTTP client — markets, quotes, wallet, trades, spot price, F&G index, retry logic
├── strategy/strategy.go     Strategy interface + 4 implementations (nearest, feargreed, bestodds, followcrowd)
├── model/model.go           Forecaster interface — gaussian, skewed, uniform, lognormal, HTTP & JSON-file models
├── kelly/optimizer.go       Fractional-Kelly portfolio optimizer (screening, caps, gradient ascent, execution)
├── lmsr/engine.go           QLS-LMSR market-maker engine — cost function, prices, implied probabilities, bin math
├── bot/bot.go               Tick loop / state machine — strategy mode & Kelly mode, NoDuplicates fallback, trade execution
├── tui/                     Bloomberg-style terminal dashboard (live histogram, Kelly panel, trade log)
├── web/
│   └── handlers.go          HTTP route handlers, CORS, embedded dashboard
├── frontend/                Next.js 14 dashboard (primary UI)
│   ├── app/
│   │   ├── page.tsx         Main dashboard page
│   │   └── layout.tsx       Root layout with global styles
│   ├── components/
│   │   ├── ConfigCard.tsx   Strategy/Kelly mode toggle, model picker, sliders, range picker, quick trade
│   │   ├── StatsGrid.tsx    Live stats cards (balance, spot, F&G, trades)
│   │   ├── TradeTable.tsx   Session trade history table
│   │   └── LogBox.tsx       Scrolling bot log viewer
│   ├── lib/
│   │   ├── api.ts           Typed fetch wrappers for all backend endpoints
│   │   └── types.ts         Shared TypeScript interfaces (incl. Kelly/model config & summaries)
│   ├── next.config.mjs      Proxy: /api/* → localhost:3002
│   └── package.json
├── docs/                    Research papers behind the Kelly/LMSR design (see below)
├── go.mod
└── README.md
```

---

## Research & References

The `docs/` directory contains the academic foundations behind the bot's quantitative core:

- **`benter_paper.pdf`** — Bill Benter's 1994 paper *"Computer Based Horse Race Handicapping and Wagering Systems"*, the origin of the fractional-Kelly staking approach used by `kelly/optimizer.go`.
- **`Glimpse__BTC_QLS_LMSR_Whitepaper (V2).pdf`** — Glimpse's whitepaper describing the QLS-LMSR market mechanism that `lmsr/engine.go` implements and that the Kelly optimizer trades against.

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
<img width="525" height="425" alt="Screenshot 2026-05-25 at 2 32 07 AM" src="https://github.com/user-attachments/assets/0e814df3-1416-4e32-8799-2eb597e29279" />
