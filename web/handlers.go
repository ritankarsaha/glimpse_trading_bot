package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/bot"
	"github.com/ritankarsaha/glimpse-bot/config"
	"github.com/ritankarsaha/glimpse-bot/strategy"
)

//go:embed static
var staticFiles embed.FS

type Handler struct {
	client *api.GlimpseClient
	cfg    *config.Config
	mu     sync.Mutex
	bot    *bot.Bot
}

func NewHandler(mux *http.ServeMux, client *api.GlimpseClient, cfg *config.Config) *Handler {
	h := &Handler{client: client, cfg: cfg}

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("embedding static files: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// API routes.
	mux.HandleFunc("/api/ping", h.cors(h.handlePing))
	mux.HandleFunc("/api/status", h.cors(h.handleStatus))
	mux.HandleFunc("/api/trades", h.cors(h.handleTrades))
	mux.HandleFunc("/api/start", h.cors(h.handleStart))
	mux.HandleFunc("/api/stop", h.cors(h.handleStop))
	mux.HandleFunc("/api/token", h.cors(h.handleToken))
	mux.HandleFunc("/api/wallet", h.cors(h.handleWallet))
	mux.HandleFunc("/api/spot", h.cors(h.handleSpot))
	mux.HandleFunc("/api/markets", h.cors(h.handleMarkets))
	mux.HandleFunc("/api/quotes", h.cors(h.handleQuotes))
	mux.HandleFunc("/api/quicktrade", h.cors(h.handleQuickTrade))

	return h
}

func (h *Handler) cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[WEB] JSON encode error: %v", err)
	}
}

func errJSON(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// GET /api/ping 
func (h *Handler) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
}

// GET /api/status 
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	b := h.bot
	h.mu.Unlock()

	if b == nil {
		writeJSON(w, bot.BotState{})
		return
	}
	writeJSON(w, b.GetState())
}

// GET /api/trades 
func (h *Handler) handleTrades(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	b := h.bot
	h.mu.Unlock()

	if b == nil {
		writeJSON(w, []bot.TradeRecord{})
		return
	}
	writeJSON(w, b.GetTradelog())
}

// POST /api/start 
func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TopicID        int     `json:"topic_id"`
		Contracts      int     `json:"contracts"`
		Strategy       string  `json:"strategy"`
		PollSec        int     `json:"poll_sec"`
		MaxTrades      int     `json:"max_trades"`
		DryRun         bool    `json:"dry_run"`
		MinExpiryMins  int     `json:"min_expiry_mins"`
		MaxWalletPct   int     `json:"max_wallet_pct"`
		NoDuplicates   bool    `json:"no_duplicates"`
		ForcedOptionID int     `json:"forced_option_id"`
		// Kelly mode — set "model" to activate the 4-stage QLS-LMSR pipeline
		Model         string  `json:"model"`         
		SigmaPct      float64 `json:"sigma_pct"`    
		KellyFraction float64 `json:"kelly_fraction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}

	if req.TopicID == 0 {
		req.TopicID = h.cfg.TopicID
	}
	if req.Contracts == 0 {
		req.Contracts = h.cfg.Contracts
	}
	if req.PollSec == 0 {
		req.PollSec = h.cfg.PollSec
	}
	if req.MaxTrades == 0 {
		req.MaxTrades = h.cfg.MaxTrades
	}
	if req.MinExpiryMins == 0 {
		req.MinExpiryMins = h.cfg.MinExpiryMins
	}
	if req.MaxWalletPct == 0 {
		req.MaxWalletPct = h.cfg.MaxWalletPct
	}

	// Decide mode: Kelly when "model" is provided, otherwise strategy
	var strat strategy.Strategy
	if req.Model == "" {
		// Strategy mode (existing behaviour)
		if req.Strategy == "" {
			req.Strategy = h.cfg.Strategy
		}
		var err error
		strat, err = strategy.FromName(req.Strategy)
		if err != nil {
			errJSON(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		// Kelly mode — apply default Kelly fraction if caller omitted it
		if req.KellyFraction <= 0 {
			req.KellyFraction = 0.25
		}
	}

	cfg := bot.BotConfig{
		TopicID:        req.TopicID,
		Contracts:      req.Contracts,
		PollSec:        req.PollSec,
		MaxTrades:      req.MaxTrades,
		DryRun:         req.DryRun,
		MinExpiryMins:  req.MinExpiryMins,
		MaxWalletPct:   req.MaxWalletPct,
		NoDuplicates:   req.NoDuplicates,
		ForcedOptionID: req.ForcedOptionID,
		ModelName:      req.Model,
		SigmaPct:       req.SigmaPct,
		KellyFraction:  req.KellyFraction,
	}

	h.mu.Lock()
	if h.bot != nil && h.bot.IsRunning() {
		h.mu.Unlock()
		errJSON(w, "bot is already running — stop it first", http.StatusConflict)
		return
	}
	h.bot = bot.NewBot(h.client, strat, cfg)
	b := h.bot
	h.mu.Unlock()

	if err := b.Start(); err != nil {
		errJSON(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "started"})
}

// POST /api/stop 
func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.mu.Lock()
	b := h.bot
	h.mu.Unlock()

	if b == nil || !b.IsRunning() {
		errJSON(w, "bot is not running", http.StatusBadRequest)
		return
	}
	b.Stop()
	writeJSON(w, map[string]string{"status": "stopped"})
}

// POST /api/token
func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		errJSON(w, "token must not be empty", http.StatusBadRequest)
		return
	}

	h.client.SetToken(req.Token)
	h.mu.Lock()
	b := h.bot
	h.mu.Unlock()
	if b != nil && b.IsPaused() {
		b.Resume()
	}

	writeJSON(w, map[string]string{"status": "token updated"})
}

// GET /api/wallet
func (h *Handler) handleWallet(w http.ResponseWriter, r *http.Request) {
	wb, err := h.client.GetWalletBalance()
	if err != nil {
		errJSON(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, wb)
}

// GET /api/markets 
func (h *Handler) handleMarkets(w http.ResponseWriter, r *http.Request) {
	markets, err := h.client.GetActiveMarkets()
	if err != nil {
		errJSON(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, markets)
}

func (h *Handler) handleQuotes(w http.ResponseWriter, r *http.Request) {
	topicID, err := strconv.Atoi(r.URL.Query().Get("topic_id"))
	if err != nil || topicID <= 0 {
		errJSON(w, "topic_id query param required", http.StatusBadRequest)
		return
	}
	market, err := h.client.GetQuotes(topicID)
	if err != nil {
		if api.IsUnauthorized(err) {
			errJSON(w, "JWT expired — paste a fresh token first", http.StatusUnauthorized)
			return
		}
		errJSON(w, fmt.Sprintf("fetching quotes: %v", err), http.StatusBadGateway)
		return
	}
	writeJSON(w, market.Outcomes)
}

// POST /api/quicktrade — single immediate trade on a specific option_id, no bot session needed
func (h *Handler) handleQuickTrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errJSON(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TopicID    int  `json:"topic_id"`
		OptionID   int  `json:"option_id"`
		Contracts  int  `json:"contracts"`
		DryRun     bool `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if req.TopicID == 0 {
		errJSON(w, "topic_id is required", http.StatusBadRequest)
		return
	}
	if req.OptionID == 0 {
		errJSON(w, "option_id is required — select a range first", http.StatusBadRequest)
		return
	}
	if req.Contracts <= 0 {
		req.Contracts = 1
	}

	// Fetch live quotes to resolve the option details
	market, err := h.client.GetQuotes(req.TopicID)
	if err != nil {
		if api.IsUnauthorized(err) {
			errJSON(w, "JWT expired — paste a fresh token first", http.StatusUnauthorized)
			return
		}
		errJSON(w, fmt.Sprintf("fetching quotes: %v", err), http.StatusBadGateway)
		return
	}
	if market.IsResolved {
		errJSON(w, "market is already resolved", http.StatusBadRequest)
		return
	}

	var outcome *api.Outcome
	for i := range market.Outcomes {
		if market.Outcomes[i].OptionID == req.OptionID {
			outcome = &market.Outcomes[i]
			break
		}
	}
	if outcome == nil {
		errJSON(w, fmt.Sprintf("option_id %d not found in market %d", req.OptionID, req.TopicID), http.StatusBadRequest)
		return
	}

	type result struct {
		DryRun              bool   `json:"dry_run"`
		TradeID             string `json:"trade_id,omitempty"`
		OptionID            int    `json:"option_id"`
		Range               string `json:"range"`
		Contracts           int    `json:"contracts"`
		CostMillisats       int64  `json:"cost_millisats"`
		CommissionMillisats int64  `json:"commission_millisats"`
		YesPriceMillisats   int64  `json:"yes_price_millisats"`
	}

	if req.DryRun {
		writeJSON(w, result{
			DryRun:            true,
			OptionID:          outcome.OptionID,
			Range:             outcome.Name,
			Contracts:         req.Contracts,
			YesPriceMillisats: outcome.YesPriceMillisats,
		})
		return
	}

	tradeReq := api.TradeRequest{
		Topics: []api.TopicTrade{{
			TopicID: req.TopicID,
			Legs:    []api.Leg{{OptionID: outcome.OptionID, Contracts: req.Contracts}},
		}},
	}
	resp, err := h.client.PlaceTrade(tradeReq)
	if err != nil {
		if api.IsUnauthorized(err) {
			errJSON(w, "JWT expired — paste a fresh token first", http.StatusUnauthorized)
			return
		}
		errJSON(w, fmt.Sprintf("placing trade: %v", err), http.StatusBadGateway)
		return
	}
	if resp.TopicsFailed > 0 {
		errJSON(w, fmt.Sprintf("trade rejected by exchange (topics_failed=%d)", resp.TopicsFailed), http.StatusBadRequest)
		return
	}

	var costMs, commMs int64
	var tradeID string
	for _, r := range resp.Results {
		costMs += r.TotalCostMillisats
		commMs += r.CommissionMillisats
		tradeID = r.TradeID
	}

	log.Printf("[WEB] quick trade: topic=%d option=%d range=%s contracts=%d id=%s",
		req.TopicID, outcome.OptionID, outcome.Name, req.Contracts, tradeID)

	writeJSON(w, result{
		DryRun:              false,
		TradeID:             tradeID,
		OptionID:            outcome.OptionID,
		Range:               outcome.Name,
		Contracts:           req.Contracts,
		CostMillisats:       costMs,
		CommissionMillisats: commMs,
		YesPriceMillisats:   outcome.YesPriceMillisats,
	})
}

// GET /api/spot 
func (h *Handler) handleSpot(w http.ResponseWriter, r *http.Request) {
	price, err := h.client.GetBTCSpot()
	if err != nil {
		errJSON(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]float64{"price": price})
}
