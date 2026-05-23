package bot

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/strategy"
)

type BotConfig struct {
	TopicID        int
	Contracts      int
	PollSec        int
	MaxTrades      int
	DryRun         bool
	MinExpiryMins  int  
	MaxWalletPct   int  
	NoDuplicates   bool 
	ForcedOptionID int  
}

type BotState struct {
	Running            bool      `json:"running"`
	Paused             bool      `json:"paused"`
	SessionTrades      int       `json:"session_trades"`
	TotalCostMillisats int64     `json:"total_cost_millisats"`
	LastSpot           float64   `json:"last_spot"`
	LastFng            int       `json:"last_fng"`
	LastFngLabel       string    `json:"last_fng_label"`
	WalletBalance      int64     `json:"wallet_balance"`
	LastError          string    `json:"last_error"`
	LastTradeTime      time.Time `json:"last_trade_time"`
	RecentLogs         []string  `json:"recent_logs"`
	DryRun             bool      `json:"dry_run"`
}

type TradeRecord struct {
	TradeID             string    `json:"trade_id"`
	TopicID             int       `json:"topic_id"`
	OptionID            int       `json:"option_id"`
	Range               string    `json:"range"`
	Contracts           int       `json:"contracts"`
	CostMillisats       int64     `json:"cost_millisats"`
	CommissionMillisats int64     `json:"commission_millisats"`
	Timestamp           time.Time `json:"timestamp"`
	DryRun              bool      `json:"dry_run"`
}

type Bot struct {
	client        *api.GlimpseClient
	strat         strategy.Strategy
	cfg           BotConfig
	mu            sync.RWMutex
	state         BotState
	logMu         sync.Mutex
	logs          []string
	tradelog      []TradeRecord
	tradedOptions map[int]bool
	cancel        context.CancelFunc
}

func NewBot(client *api.GlimpseClient, strat strategy.Strategy, cfg BotConfig) *Bot {
	return &Bot{
		client:        client,
		strat:         strat,
		cfg:           cfg,
		tradedOptions: make(map[int]bool),
	}
}

func (b *Bot) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Running {
		return fmt.Errorf("bot is already running")
	}
	if !b.client.HasToken() {
		return fmt.Errorf("no JWT token configured — paste one via the UI first")
	}

	b.state = BotState{Running: true, DryRun: b.cfg.DryRun}
	b.tradelog = nil
	b.logs = nil
	b.tradedOptions = make(map[int]bool)

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	go b.run(ctx)
	return nil
}

func (b *Bot) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.state.Running = false
}

func (b *Bot) Resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.Paused = false
	b.state.LastError = ""
}

func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state.Running
}

func (b *Bot) IsPaused() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state.Paused
}

func (b *Bot) GetState() BotState {
	b.logMu.Lock()
	logsCopy := make([]string, len(b.logs))
	copy(logsCopy, b.logs)
	b.logMu.Unlock()

	b.mu.RLock()
	s := b.state
	b.mu.RUnlock()

	s.RecentLogs = logsCopy
	return s
}

func (b *Bot) GetTradelog() []TradeRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]TradeRecord, len(b.tradelog))
	copy(out, b.tradelog)
	return out
}

func (b *Bot) run(ctx context.Context) {
	defer func() {
		b.mu.Lock()
		b.state.Running = false
		b.mu.Unlock()
		b.appendLog("Bot stopped")
	}()

	b.appendLog(fmt.Sprintf(
		"Bot started — strategy=%s topicID=%d contracts=%d dryRun=%v minExpiry=%dm maxWalletPct=%d%% noDuplicates=%v",
		b.strat.Name(), b.cfg.TopicID, b.cfg.Contracts, b.cfg.DryRun,
		b.cfg.MinExpiryMins, b.cfg.MaxWalletPct, b.cfg.NoDuplicates,
	))

	b.tick(ctx)
	if b.maxTradesReached() {
		b.appendLog(fmt.Sprintf("Max trades (%d) reached — stopping", b.cfg.MaxTrades))
		return
	}

	ticker := time.NewTicker(time.Duration(b.cfg.PollSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tick(ctx)
			if b.maxTradesReached() {
				b.appendLog(fmt.Sprintf("Max trades (%d) reached — stopping", b.cfg.MaxTrades))
				return
			}
		}
	}
}

func (b *Bot) tick(ctx context.Context) {
	if b.IsPaused() {
		b.appendLog("Paused — waiting for fresh JWT token")
		return
	}

	// Fetch spot price
	spot, err := b.client.GetBTCSpot()
	if err != nil {
		b.setError(fmt.Sprintf("fetching BTC spot: %v", err))
		return
	}

	// Fear & Greed index
	fng, fngLabel, err := b.client.GetFearAndGreed()
	if err != nil {
		fng, fngLabel = 50, "Neutral (F&G unavailable)"
		b.appendLog(fmt.Sprintf("F&G unavailable (%v) — using neutral=50", err))
	}

	// Wallet balance
	wallet, err := b.client.GetWalletBalance()
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("JWT expired — paste a fresh token in the UI")
			return
		}
		b.setError(fmt.Sprintf("fetching wallet: %v", err))
		return
	}
	if wallet.Balance == 0 {
		b.appendLog("Wallet empty — skipping tick")
		b.mu.Lock()
		b.state.WalletBalance = 0
		b.mu.Unlock()
		return
	}

	// Market quotes
	market, err := b.client.GetQuotes(b.cfg.TopicID)
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("JWT expired — paste a fresh token in the UI")
			return
		}
		b.setError(fmt.Sprintf("fetching quotes for topic %d: %v", b.cfg.TopicID, err))
		return
	}

	// Guard: already resolved
	if market.IsResolved {
		b.appendLog(fmt.Sprintf("Market %d already resolved, skipping", b.cfg.TopicID))
		return
	}

	expiry := time.Unix(market.MarketEndTimeUTC, 0)

	if time.Now().After(expiry.Add(5 * time.Minute)) {
		b.appendLog(fmt.Sprintf("Market expired at %s, skipping", expiry.Format("15:04:05")))
		return
	}

	// Guard: too close to expiry
	if b.cfg.MinExpiryMins > 0 {
		until := time.Until(expiry)
		minDur := time.Duration(b.cfg.MinExpiryMins) * time.Minute
		if until < minDur {
			b.appendLog(fmt.Sprintf("Market expires in %s (< %dm guard) — skipping tick",
				until.Round(time.Second), b.cfg.MinExpiryMins))
			return
		}
	}

	var outcome *api.Outcome
	if b.cfg.ForcedOptionID > 0 {
		for i := range market.Outcomes {
			if market.Outcomes[i].OptionID == b.cfg.ForcedOptionID {
				outcome = &market.Outcomes[i]
				break
			}
		}
		if outcome == nil {
			b.setError(fmt.Sprintf("forced option_id=%d not found in market %d", b.cfg.ForcedOptionID, b.cfg.TopicID))
			return
		}
	} else {
		var err error
		outcome, err = b.strat.SelectOption(market.Outcomes, spot, fng)
		if err != nil {
			b.setError(fmt.Sprintf("strategy %q: %v", b.strat.Name(), err))
			return
		}
	}

	if b.cfg.NoDuplicates {
		b.mu.RLock()
		alreadyTraded := b.tradedOptions[outcome.OptionID]
		b.mu.RUnlock()
		if alreadyTraded {
			if b.cfg.ForcedOptionID > 0 {
				b.appendLog(fmt.Sprintf("Option %d (range=%s) already traded this session — skipping (NoDuplicates, forced)",
					outcome.OptionID, outcome.Name))
				return
			}
			ranked := strategy.RankByProximity(market.Outcomes, spot)
			var fallback *api.Outcome
			b.mu.RLock()
			for _, r := range ranked {
				if !b.tradedOptions[r.Outcome.OptionID] {
					fallback = r.Outcome
					break
				}
			}
			b.mu.RUnlock()
			if fallback == nil {
				b.appendLog("All ranges already traded this session — skipping (NoDuplicates)")
				return
			}
			b.appendLog(fmt.Sprintf("Option %d (range=%s) already traded — falling back to option %d (range=%s)",
				outcome.OptionID, outcome.Name, fallback.OptionID, fallback.Name))
			outcome = fallback
		}
	}

	// Guard: max wallet percentage
	if b.cfg.MaxWalletPct > 0 {
		estimatedCostMillisats := outcome.YesPriceMillisats * int64(b.cfg.Contracts)
		walletMillisats := wallet.Balance
		maxAllowedMillisats := walletMillisats * int64(b.cfg.MaxWalletPct) / 100
		if estimatedCostMillisats > maxAllowedMillisats {
			b.appendLog(fmt.Sprintf(
				"Trade cost ~%dsats exceeds %d%% wallet limit (%dsats) — skipping",
				estimatedCostMillisats/1000,
				b.cfg.MaxWalletPct,
				maxAllowedMillisats/1000,
			))
			return
		}
	}

	// Update shared state
	b.mu.Lock()
	b.state.LastSpot = spot
	b.state.LastFng = fng
	b.state.LastFngLabel = fngLabel
	b.state.WalletBalance = wallet.Balance / 1000 // millisats → sats for display
	b.state.LastError = ""
	b.mu.Unlock()

	timeUntil := time.Until(expiry).Round(time.Second)
	b.appendLog(fmt.Sprintf(
		"tick: spot=$%.2f fng=%d(%s) expiry=%s → option %d range=%s yesPrice=%dms",
		spot, fng, fngLabel, timeUntil, outcome.OptionID, outcome.Name, outcome.YesPriceMillisats,
	))

	tradeReq := api.TradeRequest{
		Topics: []api.TopicTrade{
			{
				TopicID: b.cfg.TopicID,
				Legs:    []api.Leg{{OptionID: outcome.OptionID, Contracts: b.cfg.Contracts}},
			},
		},
	}

	if b.cfg.DryRun {
		b.executeDryRun(outcome, spot, fng)
		return
	}

	resp, err := b.client.PlaceTrade(tradeReq)
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("JWT expired — paste a fresh token in the UI")
			return
		}
		b.setError(fmt.Sprintf("placing trade: %v", err))
		return
	}

	if resp.TopicsFailed > 0 {
		b.setError(fmt.Sprintf("trade rejected (topics_failed=%d): %+v", resp.TopicsFailed, resp))
		log.Printf("[BOT] trade rejected: %+v", resp)
		return
	}

	var costMs, commMs int64
	var tradeID string
	for _, r := range resp.Results {
		costMs += r.TotalCostMillisats
		commMs += r.CommissionMillisats
		tradeID = r.TradeID
	}

	record := TradeRecord{
		TradeID:             tradeID,
		TopicID:             b.cfg.TopicID,
		OptionID:            outcome.OptionID,
		Range:               outcome.Name,
		Contracts:           b.cfg.Contracts,
		CostMillisats:       costMs,
		CommissionMillisats: commMs,
		Timestamp:           time.Now(),
		DryRun:              false,
	}

	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.tradedOptions[outcome.OptionID] = true
	b.state.SessionTrades++
	b.state.TotalCostMillisats += costMs + commMs
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()

	b.appendLog(fmt.Sprintf("Trade placed: id=%s range=%s cost=%dsats comm=%dsats",
		tradeID, outcome.Name, costMs/1000, commMs/1000))
}

func (b *Bot) executeDryRun(outcome *api.Outcome, spot float64, fng int) {
	msg := fmt.Sprintf("[DRY RUN] Would trade topic=%d option=%d range=%s contracts=%d (spot=$%.2f fng=%d yesPrice=%dms)",
		b.cfg.TopicID, outcome.OptionID, outcome.Name, b.cfg.Contracts, spot, fng, outcome.YesPriceMillisats)
	b.appendLog(msg)
	log.Println(msg)

	record := TradeRecord{
		TradeID:   fmt.Sprintf("dry-%d", time.Now().UnixNano()),
		TopicID:   b.cfg.TopicID,
		OptionID:  outcome.OptionID,
		Range:     outcome.Name,
		Contracts: b.cfg.Contracts,
		Timestamp: time.Now(),
		DryRun:    true,
	}

	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.tradedOptions[outcome.OptionID] = true
	b.state.SessionTrades++
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()
}

func (b *Bot) maxTradesReached() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state.SessionTrades >= b.cfg.MaxTrades
}

func (b *Bot) setPaused(msg string) {
	b.mu.Lock()
	b.state.Paused = true
	b.state.LastError = msg
	b.mu.Unlock()
	b.appendLog("[PAUSED] " + msg)
	log.Printf("[BOT] paused: %s", msg)
}

func (b *Bot) setError(msg string) {
	b.mu.Lock()
	b.state.LastError = msg
	b.mu.Unlock()
	b.appendLog("[ERROR] " + msg)
	log.Printf("[BOT] error: %s", msg)
}

func (b *Bot) appendLog(msg string) {
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	b.logMu.Lock()
	defer b.logMu.Unlock()
	b.logs = append(b.logs, entry)
	if len(b.logs) > 100 {
		b.logs = b.logs[len(b.logs)-100:]
	}
}
