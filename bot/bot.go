package bot

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/backtest"
	"github.com/ritankarsaha/glimpse-bot/kelly"
	"github.com/ritankarsaha/glimpse-bot/lmsr"
	"github.com/ritankarsaha/glimpse-bot/model"
	"github.com/ritankarsaha/glimpse-bot/portfolio"
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

	// Kelly mode (activated when KellyFraction > 0)
	ModelName     string
	ModelURL      string  // HTTP forecaster endpoint; takes precedence over ModelName when set
	SigmaPct      float64
	KellyFraction float64
	AnnualVolPct  float64 // annualised vol in %; 0 falls back to SigmaPct
	MinEdge       float64 // minimum edge threshold; 0 uses kelly.DefaultConfig value
	MaxBins       int     // max bins per Kelly round; 0 uses kelly.DefaultConfig value

	// Multi-market portfolio mode (activated when MultiMarket is true; implies Kelly)
	MultiMarket     bool
	ThemeCapPct     float64 // % of budget allowed across all open markets combined; 0 uses portfolio.DefaultConfig value
	PerMarketCapPct float64 // % of budget allowed in any single market; 0 uses portfolio.DefaultConfig value

	// Calibration / out-of-sample recording
	RecordSamples   bool
	SamplesPath     string
	CalibrationFile string
}

type KellyTradeSummary struct {
	BinIndex  int     `json:"bin_index"`
	Range     string  `json:"range"`
	MarketPct float64 `json:"market_pct"`
	ModelPct  float64 `json:"model_pct"`
	EdgePct   float64 `json:"edge_pct"`
	Contracts int     `json:"contracts"`
}

type KellyLeg struct {
	BinIndex  int     `json:"bin_index"`
	Range     string  `json:"range"`
	Contracts int     `json:"contracts"`
	EdgePct   float64 `json:"edge_pct"`
}

type BotState struct {
	Running            bool                `json:"running"`
	Paused             bool                `json:"paused"`
	SessionTrades      int                 `json:"session_trades"`
	TotalCostMillisats int64               `json:"total_cost_millisats"`
	LastSpot           float64             `json:"last_spot"`
	LastFng            int                 `json:"last_fng"`
	LastFngLabel       string              `json:"last_fng_label"`
	WalletBalance      int64               `json:"wallet_balance"`
	LastError          string              `json:"last_error"`
	LastTradeTime      time.Time           `json:"last_trade_time"`
	RecentLogs         []string            `json:"recent_logs"`
	DryRun             bool                `json:"dry_run"`
	ModelName     string              `json:"model_name,omitempty"`
	KellyFraction float64             `json:"kelly_fraction,omitempty"`
	MispriceCount int                 `json:"misprice_count"`
	KellyTrades   []KellyTradeSummary `json:"kelly_trades,omitempty"`
}

type TradeRecord struct {
	TradeID             string     `json:"trade_id"`
	TopicID             int        `json:"topic_id"`
	OptionID            int        `json:"option_id"`
	Range               string     `json:"range"`
	Contracts           int        `json:"contracts"`
	CostMillisats       int64      `json:"cost_millisats"`
	CommissionMillisats int64      `json:"commission_millisats"`
	Timestamp           time.Time  `json:"timestamp"`
	DryRun              bool       `json:"dry_run"`
	KellyLegs           []KellyLeg `json:"kelly_legs,omitempty"`
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

	forecaster   model.Forecaster
	kellyCfg     kelly.Config
	portfolioCfg portfolio.Config
	recorder     *backtest.Recorder
	reconciled   map[int]bool
}

func NewBot(client *api.GlimpseClient, strat strategy.Strategy, cfg BotConfig) *Bot {
	return &Bot{
		client:        client,
		strat:         strat,
		cfg:           cfg,
		tradedOptions: make(map[int]bool),
		reconciled:    make(map[int]bool),
	}
}

func (b *Bot) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Running {
		return fmt.Errorf("bot is already running")
	}
	if !b.client.HasAPIKey() {
		return fmt.Errorf("no API key configured — set GLIMPSE_API_KEY or paste one via the UI first")
	}

	if b.cfg.KellyFraction > 0 || b.cfg.MultiMarket {
		name := b.cfg.ModelURL // HTTP endpoint takes precedence
		if name == "" {
			name = b.cfg.ModelName
		}
		if name == "" {
			name = "gaussian"
		}
		sigma := b.cfg.SigmaPct
		if sigma <= 0 {
			sigma = 5.0
		}
		f, err := model.FromNameWithCalibration(name, sigma, b.cfg.CalibrationFile)
		if err != nil {
			return fmt.Errorf("forecast model: %w", err)
		}
		b.forecaster = f
		b.kellyCfg = kelly.DefaultConfig()
		if b.cfg.KellyFraction > 0 {
			b.kellyCfg.KellyFraction = b.cfg.KellyFraction
		}
		if b.cfg.MinEdge > 0 {
			b.kellyCfg.MinEdge = b.cfg.MinEdge
		}
		if b.cfg.MaxBins > 0 {
			b.kellyCfg.MaxBins = b.cfg.MaxBins
		}

		b.portfolioCfg = portfolio.DefaultConfig()
		b.portfolioCfg.KellyCfg = b.kellyCfg
		if b.cfg.ThemeCapPct > 0 {
			b.portfolioCfg.ThemeCapPct = b.cfg.ThemeCapPct / 100.0
		}
		if b.cfg.PerMarketCapPct > 0 {
			b.portfolioCfg.PerMarketCapPct = b.cfg.PerMarketCapPct / 100.0
		}
	}

	if b.cfg.RecordSamples && b.cfg.SamplesPath != "" {
		b.recorder = backtest.NewRecorder(b.cfg.SamplesPath)
	} else {
		b.recorder = nil
	}

	b.state = BotState{
		Running:       true,
		DryRun:        b.cfg.DryRun,
		ModelName:     b.cfg.ModelName,
		KellyFraction: b.cfg.KellyFraction,
	}
	b.tradelog = nil
	b.logs = nil
	b.tradedOptions = make(map[int]bool)
	b.reconciled = make(map[int]bool)

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

	modeName := "strategy"
	if b.cfg.MultiMarket {
		modeName = fmt.Sprintf("portfolio(model=%s c=%.2f theme=%.0f%% market=%.0f%%)",
			b.cfg.ModelName, b.kellyCfg.KellyFraction, b.portfolioCfg.ThemeCapPct*100, b.portfolioCfg.PerMarketCapPct*100)
	} else if b.cfg.KellyFraction > 0 {
		modeName = fmt.Sprintf("kelly(model=%s c=%.2f)", b.cfg.ModelName, b.cfg.KellyFraction)
	} else if b.strat != nil {
		modeName = "strategy=" + b.strat.Name()
	}
	b.appendLog(fmt.Sprintf(
		"Bot started — %s topicID=%d dryRun=%v minExpiry=%dm maxWalletPct=%d%%",
		modeName, b.cfg.TopicID, b.cfg.DryRun, b.cfg.MinExpiryMins, b.cfg.MaxWalletPct,
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

func (b *Bot) tick(_ context.Context) {
	if b.IsPaused() {
		b.appendLog("Paused — waiting for a valid API key")
		return
	}

	spot, err := b.client.GetBTCSpot()
	if err != nil {
		b.setError(fmt.Sprintf("fetching BTC spot: %v", err))
		return
	}

	fng, fngLabel, err := b.client.GetFearAndGreed()
	if err != nil {
		fng, fngLabel = 50, "Neutral (F&G unavailable)"
		b.appendLog(fmt.Sprintf("F&G unavailable (%v) — using neutral=50", err))
	}

	wallet, err := b.client.GetWalletBalance()
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] wallet: %v — will retry next tick", err))
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

	b.mu.Lock()
	b.state.LastSpot = spot
	b.state.LastFng = fng
	b.state.LastFngLabel = fngLabel
	b.state.WalletBalance = wallet.Balance / 1000
	b.state.LastError = ""
	b.mu.Unlock()

	if b.cfg.MultiMarket {
		b.tickPortfolio(spot, fng, fngLabel, wallet)
		return
	}

	market, err := b.client.GetQuotes(b.cfg.TopicID)
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] quotes: %v — will retry next tick", err))
			return
		}
		b.setError(fmt.Sprintf("fetching quotes for topic %d: %v", b.cfg.TopicID, err))
		return
	}

	if market.IsResolved {
		b.appendLog(fmt.Sprintf("Market %d already resolved, skipping", b.cfg.TopicID))
		b.reconcileResolved(b.cfg.TopicID, time.Unix(market.MarketEndTimeUTC, 0), spot)
		return
	}

	expiry := time.Unix(market.MarketEndTimeUTC, 0)
	if time.Now().After(expiry.Add(5 * time.Minute)) {
		b.appendLog(fmt.Sprintf("Market expired at %s, skipping", expiry.Format("15:04:05")))
		return
	}
	if b.cfg.MinExpiryMins > 0 {
		if until := time.Until(expiry); until < time.Duration(b.cfg.MinExpiryMins)*time.Minute {
			b.appendLog(fmt.Sprintf("Market expires in %s (< %dm guard) — skipping tick",
				until.Round(time.Second), b.cfg.MinExpiryMins))
			return
		}
	}

	if b.cfg.KellyFraction > 0 {
		b.tickKelly(spot, fng, fngLabel, wallet, market, expiry)
	} else {
		b.tickStrategy(spot, fng, fngLabel, wallet, market, expiry)
	}
}

// recordSample appends a forecast/market-probability sample for topicID to
// the calibration log (no-op if recording is disabled). It records the
// fundamental forecast (not a benter-blended one) so FitLogitWeights always
// sees the underlying signal, per Benter's two-stage approach.
func (b *Bot) recordSample(topicID int, fctx model.ForecastContext, om *kelly.OutcomeMap) {
	if b.recorder == nil {
		return
	}
	fp, err := model.FundamentalProbs(b.forecaster, fctx)
	if err != nil {
		b.appendLog(fmt.Sprintf("recording sample for topic %d: %v", topicID, err))
		return
	}
	sample := backtest.Sample{
		Timestamp:        time.Now(),
		TopicID:          topicID,
		FundamentalProbs: fp,
		MarketProbs:      lmsr.ImpliedProbs(om.Q),
		OutcomeBin:       -1,
	}
	if err := b.recorder.Record(sample); err != nil {
		b.appendLog(fmt.Sprintf("recording sample for topic %d: %v", topicID, err))
	}
}

// reconcileResolved fills in the realized outcome bin for any unresolved
// recorded samples of topicID once the market has resolved, so the
// calibration harness (backtest.FitLogitWeights) can use them. Each topic is
// reconciled at most once per bot run.
func (b *Bot) reconcileResolved(topicID int, expiry time.Time, spot float64) {
	if b.recorder == nil || b.reconciled[topicID] {
		return
	}
	if err := backtest.Reconcile(b.cfg.SamplesPath, topicID, expiry, lmsr.SpotToBin(spot)); err != nil {
		b.appendLog(fmt.Sprintf("reconcile topic %d: %v", topicID, err))
		return
	}
	b.reconciled[topicID] = true
}

func (b *Bot) tickKelly(spot float64, fng int, fngLabel string, wallet *api.WalletBalance, market *api.Market, expiry time.Time) {
	b.appendLog(fmt.Sprintf("kelly tick: spot=$%.2f fng=%d(%s) expiry=%s",
		spot, fng, fngLabel, time.Until(expiry).Round(time.Second)))

	om := kelly.BuildOutcomeMap(market.Outcomes)

	fctx := model.ForecastContext{
		SpotPrice:      spot,
		FearGreedIndex: fng,
		TimeToExpiry:   time.Until(expiry),
		ImpliedProbs:   lmsr.ImpliedProbs(om.Q),
		RealizedVolAnn: b.cfg.AnnualVolPct / 100.0,
	}
	mdlProbs, err := b.forecaster.Forecast(fctx)
	if err != nil {
		b.setError(fmt.Sprintf("forecast: %v", err))
		return
	}

	b.recordSample(b.cfg.TopicID, fctx, om)

	balSats := float64(wallet.Balance) / 1000.0
	budgetSats := balSats * float64(b.cfg.MaxWalletPct) / 100.0

	trades, err := kelly.Optimize(om, mdlProbs, budgetSats, b.kellyCfg)
	if err != nil {
		b.setError(fmt.Sprintf("kelly optimise: %v", err))
		return
	}
	summaries := make([]KellyTradeSummary, 0, len(trades))
	for _, t := range trades {
		summaries = append(summaries, KellyTradeSummary{
			BinIndex:  t.BinIndex,
			Range:     t.Range,
			MarketPct: t.MarketProb * 100,
			ModelPct:  t.ModelProb * 100,
			EdgePct:   t.Edge * 100,
			Contracts: t.Contracts,
		})
	}
	b.mu.Lock()
	b.state.MispriceCount = len(trades)
	b.state.KellyTrades = summaries
	b.mu.Unlock()

	if len(trades) == 0 {
		b.appendLog("Kelly: no underpriced bins found — skipping")
		return
	}
	b.appendLog(fmt.Sprintf("Kelly: %d underpriced bins, budget=%.0fsats", len(trades), budgetSats))

	if b.cfg.DryRun {
		b.executeKellyDryRun(trades)
		return
	}

	legs := make([]api.Leg, 0, len(trades))
	for _, t := range trades {
		legs = append(legs, api.Leg{OptionID: t.OptionID, Contracts: t.Contracts})
	}
	resp, err := b.client.PlaceTrade(api.TradeRequest{
		Topics: []api.TopicTrade{{TopicID: b.cfg.TopicID, Legs: legs}},
	})
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] place trade: %v — will retry next tick", err))
			return
		}
		b.setError(fmt.Sprintf("placing kelly trade: %v", err))
		return
	}
	if resp.TopicsFailed > 0 {
		b.setError(fmt.Sprintf("kelly trade rejected (topics_failed=%d)", resp.TopicsFailed))
		log.Printf("[BOT] kelly trade rejected: %+v", resp)
		return
	}

	var costMs, commMs int64
	var tradeID string
	for _, r := range resp.Results {
		costMs += r.TotalCostMillisats
		commMs += r.CommissionMillisats
		tradeID = r.TradeID
	}

	kellyLegs := make([]KellyLeg, 0, len(trades))
	for _, t := range trades {
		kellyLegs = append(kellyLegs, KellyLeg{
			BinIndex:  t.BinIndex,
			Range:     t.Range,
			Contracts: t.Contracts,
			EdgePct:   t.Edge * 100,
		})
	}

	record := TradeRecord{
		TradeID:             tradeID,
		TopicID:             b.cfg.TopicID,
		Range:               fmt.Sprintf("KELLY(%d legs)", len(legs)),
		CostMillisats:       costMs,
		CommissionMillisats: commMs,
		Timestamp:           time.Now(),
		KellyLegs:           kellyLegs,
	}
	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.state.SessionTrades++
	b.state.TotalCostMillisats += costMs + commMs
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()

	b.appendLog(fmt.Sprintf("Kelly trade placed: id=%s legs=%d cost=%dsats comm=%dsats",
		tradeID, len(legs), costMs/1000, commMs/1000))
}

func (b *Bot) executeKellyDryRun(trades []kelly.Trade) {
	var totalMs int64
	kellyLegs := make([]KellyLeg, 0, len(trades))
	for _, t := range trades {
		ms := t.YesPriceMillisats * int64(t.Contracts)
		totalMs += ms
		kellyLegs = append(kellyLegs, KellyLeg{
			BinIndex:  t.BinIndex,
			Range:     t.Range,
			Contracts: t.Contracts,
			EdgePct:   t.Edge * 100,
		})
		b.appendLog(fmt.Sprintf("[DRY KELLY] range=%s contracts=%d edge=%.2f%% cost=%dsats",
			t.Range, t.Contracts, t.Edge*100, ms/1000))
	}
	record := TradeRecord{
		TradeID:       fmt.Sprintf("dry-kelly-%d", time.Now().UnixNano()),
		TopicID:       b.cfg.TopicID,
		Range:         fmt.Sprintf("KELLY(%d legs)", len(trades)),
		CostMillisats: totalMs,
		Timestamp:     time.Now(),
		DryRun:        true,
		KellyLegs:     kellyLegs,
	}
	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.state.SessionTrades++
	b.state.TotalCostMillisats += totalMs
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()
}
func (b *Bot) tickPortfolio(spot float64, fng int, fngLabel string, wallet *api.WalletBalance) {
	b.appendLog(fmt.Sprintf("portfolio tick: spot=$%.2f fng=%d(%s)", spot, fng, fngLabel))

	summaries, err := b.client.GetActiveMarkets()
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] active markets: %v — will retry next tick", err))
			return
		}
		b.setError(fmt.Sprintf("fetching active markets: %v", err))
		return
	}

	var inputs []portfolio.MarketInput
	for _, ms := range summaries {
		if ms.IsResolved {
			b.reconcileResolved(ms.TopicID, time.Unix(ms.EndTimeUTC, 0), spot)
			continue
		}
		if !ms.IsActive {
			continue
		}
		expiry := time.Unix(ms.EndTimeUTC, 0)
		if time.Now().After(expiry.Add(5 * time.Minute)) {
			continue
		}
		if b.cfg.MinExpiryMins > 0 {
			if until := time.Until(expiry); until < time.Duration(b.cfg.MinExpiryMins)*time.Minute {
				continue
			}
		}

		market, err := b.client.GetQuotes(ms.TopicID)
		if err != nil {
			b.appendLog(fmt.Sprintf("portfolio: quotes for topic %d: %v — skipping market", ms.TopicID, err))
			continue
		}

		om := kelly.BuildOutcomeMap(market.Outcomes)
		fctx := model.ForecastContext{
			SpotPrice:      spot,
			FearGreedIndex: fng,
			TimeToExpiry:   time.Until(expiry),
			ImpliedProbs:   lmsr.ImpliedProbs(om.Q),
			RealizedVolAnn: b.cfg.AnnualVolPct / 100.0,
		}
		mdlProbs, err := b.forecaster.Forecast(fctx)
		if err != nil {
			b.appendLog(fmt.Sprintf("portfolio: forecast for topic %d: %v — skipping market", ms.TopicID, err))
			continue
		}

		b.recordSample(ms.TopicID, fctx, om)
		inputs = append(inputs, portfolio.MarketInput{TopicID: ms.TopicID, OutcomeMap: om, ModelProbs: mdlProbs})
	}

	if len(inputs) == 0 {
		b.appendLog("Portfolio: no eligible markets")
		b.mu.Lock()
		b.state.MispriceCount = 0
		b.state.KellyTrades = nil
		b.mu.Unlock()
		return
	}

	balSats := float64(wallet.Balance) / 1000.0
	budgetSats := balSats * float64(b.cfg.MaxWalletPct) / 100.0

	allocations, err := portfolio.Allocate(inputs, budgetSats, b.portfolioCfg)
	if err != nil {
		b.setError(fmt.Sprintf("portfolio allocate: %v", err))
		return
	}

	if len(allocations) == 0 {
		b.appendLog("Portfolio: no underpriced bins found across active markets — skipping")
		b.mu.Lock()
		b.state.MispriceCount = 0
		b.state.KellyTrades = nil
		b.mu.Unlock()
		return
	}

	// Iterate topics in a deterministic order (map iteration order is random).
	topicIDs := make([]int, 0, len(allocations))
	for topicID := range allocations {
		topicIDs = append(topicIDs, topicID)
	}
	sort.Ints(topicIDs)

	var summariesOut []KellyTradeSummary
	var totalLegs int
	topics := make([]api.TopicTrade, 0, len(topicIDs))
	for _, topicID := range topicIDs {
		trades := allocations[topicID]
		legs := make([]api.Leg, 0, len(trades))
		for _, t := range trades {
			legs = append(legs, api.Leg{OptionID: t.OptionID, Contracts: t.Contracts})
			summariesOut = append(summariesOut, KellyTradeSummary{
				BinIndex:  t.BinIndex,
				Range:     fmt.Sprintf("topic %d: %s", topicID, t.Range),
				MarketPct: t.MarketProb * 100,
				ModelPct:  t.ModelProb * 100,
				EdgePct:   t.Edge * 100,
				Contracts: t.Contracts,
			})
			totalLegs++
		}
		topics = append(topics, api.TopicTrade{TopicID: topicID, Legs: legs})
	}

	b.mu.Lock()
	b.state.MispriceCount = totalLegs
	b.state.KellyTrades = summariesOut
	b.mu.Unlock()

	b.appendLog(fmt.Sprintf("Portfolio: %d markets, %d legs, budget=%.0fsats (theme cap=%.0f%% market cap=%.0f%%)",
		len(topics), totalLegs, budgetSats, b.portfolioCfg.ThemeCapPct*100, b.portfolioCfg.PerMarketCapPct*100))

	if b.cfg.DryRun {
		b.executePortfolioDryRun(allocations, topicIDs)
		return
	}

	resp, err := b.client.PlaceTrade(api.TradeRequest{Topics: topics})
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] place portfolio trade: %v — will retry next tick", err))
			return
		}
		b.setError(fmt.Sprintf("placing portfolio trade: %v", err))
		return
	}
	if resp.TopicsFailed > 0 {
		b.setError(fmt.Sprintf("portfolio trade rejected (topics_failed=%d)", resp.TopicsFailed))
		log.Printf("[BOT] portfolio trade rejected: %+v", resp)
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
		Range:               fmt.Sprintf("PORTFOLIO(%d markets, %d legs)", len(topics), totalLegs),
		CostMillisats:       costMs,
		CommissionMillisats: commMs,
		Timestamp:           time.Now(),
	}
	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.state.SessionTrades++
	b.state.TotalCostMillisats += costMs + commMs
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()

	b.appendLog(fmt.Sprintf("Portfolio trade placed: id=%s markets=%d legs=%d cost=%dsats comm=%dsats",
		tradeID, len(topics), totalLegs, costMs/1000, commMs/1000))
}

func (b *Bot) executePortfolioDryRun(allocations map[int][]kelly.Trade, topicIDs []int) {
	var totalMs int64
	var totalLegs int
	for _, topicID := range topicIDs {
		for _, t := range allocations[topicID] {
			ms := t.YesPriceMillisats * int64(t.Contracts)
			totalMs += ms
			totalLegs++
			b.appendLog(fmt.Sprintf("[DRY PORTFOLIO] topic=%d range=%s contracts=%d edge=%.2f%% cost=%dsats",
				topicID, t.Range, t.Contracts, t.Edge*100, ms/1000))
		}
	}
	record := TradeRecord{
		TradeID:       fmt.Sprintf("dry-portfolio-%d", time.Now().UnixNano()),
		Range:         fmt.Sprintf("PORTFOLIO(%d markets, %d legs)", len(topicIDs), totalLegs),
		CostMillisats: totalMs,
		Timestamp:     time.Now(),
		DryRun:        true,
	}
	b.mu.Lock()
	b.tradelog = append(b.tradelog, record)
	b.state.SessionTrades++
	b.state.TotalCostMillisats += totalMs
	b.state.LastTradeTime = record.Timestamp
	b.mu.Unlock()
}

func (b *Bot) tickStrategy(spot float64, fng int, fngLabel string, wallet *api.WalletBalance, market *api.Market, expiry time.Time) {
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
				b.appendLog(fmt.Sprintf("Option %d already traded this session — skipping (NoDuplicates, forced)", outcome.OptionID))
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
			b.appendLog(fmt.Sprintf("Option %d already traded — falling back to option %d (range=%s)",
				outcome.OptionID, fallback.OptionID, fallback.Name))
			outcome = fallback
		}
	}

	if b.cfg.MaxWalletPct > 0 {
		estimatedCostMs := outcome.YesPriceMillisats * int64(b.cfg.Contracts)
		maxAllowedMs := wallet.Balance * int64(b.cfg.MaxWalletPct) / 100
		if estimatedCostMs > maxAllowedMs {
			b.appendLog(fmt.Sprintf(
				"Trade cost ~%dsats exceeds %d%% wallet limit (%dsats) — skipping",
				estimatedCostMs/1000, b.cfg.MaxWalletPct, maxAllowedMs/1000))
			return
		}
	}

	timeUntil := time.Until(expiry).Round(time.Second)
	b.appendLog(fmt.Sprintf(
		"tick: spot=$%.2f fng=%d(%s) expiry=%s → option %d range=%s yesPrice=%dms",
		spot, fng, fngLabel, timeUntil, outcome.OptionID, outcome.Name, outcome.YesPriceMillisats,
	))

	tradeReq := api.TradeRequest{
		Topics: []api.TopicTrade{
			{TopicID: b.cfg.TopicID, Legs: []api.Leg{{OptionID: outcome.OptionID, Contracts: b.cfg.Contracts}}},
		},
	}

	if b.cfg.DryRun {
		b.executeDryRun(outcome, spot, fng)
		return
	}

	resp, err := b.client.PlaceTrade(tradeReq)
	if err != nil {
		if api.IsUnauthorized(err) {
			b.setPaused("API key unauthorized — check the key is valid in the UI")
			return
		}
		if api.IsTransient(err) {
			b.appendLog(fmt.Sprintf("[TRANSIENT] place trade: %v — will retry next tick", err))
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
