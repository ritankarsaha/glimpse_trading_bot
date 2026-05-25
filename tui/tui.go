
package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/kelly"
	"github.com/ritankarsaha/glimpse-bot/lmsr"
	"github.com/ritankarsaha/glimpse-bot/model"
)


const (
	esc   = "\033["
	reset = "\033[0m"
	bold  = "\033[1m"
	red   = "\033[31m"
	green = "\033[32m"
	yel   = "\033[33m"
	blue  = "\033[34m"
	cyan  = "\033[36m"
	white = "\033[97m"
	dim   = "\033[2m"
)

func clearScreen()              { fmt.Print("\033[2J\033[H") }
func moveTo(row, col int)       { fmt.Printf("%s%d;%dH", esc, row, col) }
func clrLine()                  { fmt.Print("\033[2K") }
func colorf(c, s string) string { return c + s + reset }
func pad(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}


type TradeRecord struct {
	Time      time.Time
	Range     string
	Contracts int
	CostSats  int64
	DryRun    bool
	TradeID   string
}


type state struct {
	spot       float64
	fng        int
	fngLabel   string
	title      string
	expiry     time.Time
	balance    int64 
	mktProbs   []float64
	mdlProbs   []float64
	trades     []kelly.Trade
	tradelog   []TradeRecord
	totalCost  int64 
	lastUpdate time.Time
	lastErr    string
	running    bool
	dryRun     bool
	logs       []string
}

type TUI struct {
	client    *api.GlimpseClient
	forecaster model.Forecaster
	kellyCfg  kelly.Config
	topicID   int
	pollSec   int
	dryRun    bool

	mu     sync.Mutex
	st     state
	cancel context.CancelFunc
	keys   chan byte
}

// Config for the TUI bot session
type Config struct {
	TopicID   int
	PollSec   int
	DryRun    bool
	Model     model.Forecaster
	KellyCfg  kelly.Config
}

// New creates a TUI instance. Call Run() to start
func New(client *api.GlimpseClient, cfg Config) *TUI {
	return &TUI{
		client:     client,
		forecaster: cfg.Model,
		kellyCfg:   cfg.KellyCfg,
		topicID:    cfg.TopicID,
		pollSec:    cfg.PollSec,
		dryRun:     cfg.DryRun,
		keys:       make(chan byte, 4),
	}
}

// Run starts the TUI, blocks until the user presses 'q'
func (t *TUI) Run() error {
	restore, err := rawMode()
	if err != nil {
		// rawMode failure is non-fatal; just lose single-keystroke support
		fmt.Fprintf(os.Stderr, "warning: raw terminal mode unavailable: %v\n", err)
	}
	defer restore()

	// Hide cursor
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h\033[0m\n")

	clearScreen()

	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel

	// Background poll goroutine
	go t.pollLoop(ctx)

	// Render refresh goroutine (independent of poll)
	go func() {
		tk := time.NewTicker(1 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				t.render()
			}
		}
	}()

	// Key reader
	go func() {
		buf := make([]byte, 1)
		for {
			n, _ := os.Stdin.Read(buf)
			if n > 0 {
				t.keys <- buf[0]
			}
		}
	}()

	t.appendLog("Glimpse Kelly Bot TUI started. Press 's' to start trading, 'q' to quit.")
	t.render()

	for {
		key, ok := <-t.keys
		if !ok {
			break
		}
		switch key {
		case 'q', 'Q', 3: // 3 = Ctrl-C
			cancel()
			clearScreen()
			moveTo(1, 1)
			return nil
		case 's', 'S':
			t.mu.Lock()
			t.st.running = !t.st.running
			if t.st.running {
				t.appendLogLocked("Trading started")
			} else {
				t.appendLogLocked("Trading paused")
			}
			t.mu.Unlock()
			t.render()
		case 'd', 'D':
			t.mu.Lock()
			t.dryRun = !t.dryRun
			t.st.dryRun = t.dryRun
			mode := "LIVE"
			if t.dryRun {
				mode = "DRY RUN"
			}
			t.appendLogLocked("Switched to " + mode + " mode")
			t.mu.Unlock()
			t.render()
		case 'r', 'R':
			go t.tick()
		}
	}
	return nil
}

func (t *TUI) pollLoop(ctx context.Context) {
	t.tick()
	tk := time.NewTicker(time.Duration(t.pollSec) * time.Second)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			t.tick()
		}
	}
}

func (t *TUI) tick() {
	// Fetch spot + F&G
	spot, err := t.client.GetBTCSpot()
	if err != nil {
		t.setErr("spot: " + err.Error())
		return
	}
	fng, fngLabel, _ := t.client.GetFearAndGreed()

	// Fetch wallet
	wallet, err := t.client.GetWalletBalance()
	if err != nil {
		if api.IsUnauthorized(err) {
			t.setErr("JWT expired — paste a fresh token via /api/token")
		} else {
			t.setErr("wallet: " + err.Error())
		}
		return
	}

	// Fetch market quotes
	market, err := t.client.GetQuotes(t.topicID)
	if err != nil {
		t.setErr(fmt.Sprintf("quotes(topic=%d): %v", t.topicID, err))
		return
	}
	if market.IsResolved {
		t.setErr(fmt.Sprintf("market %d is resolved", t.topicID))
		return
	}

	// Build quantity vector + outcome map
	om := kelly.BuildOutcomeMap(market.Outcomes)

	// Market-implied probs
	mktProbs := lmsr.ImpliedProbs(om.Q)

	// Forecast
	mdlProbs, err := t.forecaster.Forecast(spot, fng)
	if err != nil {
		t.setErr("forecast: " + err.Error())
		return
	}

	// Kelly pipeline
	balSats := float64(wallet.Balance) / 1000.0 // millisats -> sats
	var recTrades []kelly.Trade
	t.mu.Lock()
	if t.st.running {
		t.mu.Unlock()
		recTrades, err = kelly.Optimize(om, mdlProbs, balSats*float64(t.kellyCfg.KellyFraction), t.kellyCfg)
		if err != nil {
			t.setErr("kelly: " + err.Error())
		}
	} else {
		t.mu.Unlock()
		// Even when stopped, compute and display recommendations
		recTrades, _ = kelly.Optimize(om, mdlProbs, balSats*float64(t.kellyCfg.KellyFraction), t.kellyCfg)
	}

	// Execute trades if running
	t.mu.Lock()
	running := t.st.running
	dryRun := t.dryRun
	t.mu.Unlock()

	if running && len(recTrades) > 0 {
		t.executeTrades(market, recTrades, dryRun)
	}

	// Update state
	expiry := time.Unix(market.MarketEndTimeUTC, 0)
	t.mu.Lock()
	t.st.spot = spot
	t.st.fng = fng
	t.st.fngLabel = fngLabel
	t.st.title = market.Title
	t.st.expiry = expiry
	t.st.balance = wallet.Balance / 1000
	t.st.mktProbs = mktProbs
	t.st.mdlProbs = mdlProbs
	t.st.trades = recTrades
	t.st.dryRun = dryRun
	t.st.lastUpdate = time.Now()
	t.st.lastErr = ""
	t.appendLogLocked(fmt.Sprintf("tick: spot=$%.2f fng=%d(%s) %d bins in A",
		spot, fng, fngLabel, len(recTrades)))
	t.mu.Unlock()

	t.render()
}

func (t *TUI) executeTrades(market *api.Market, trades []kelly.Trade, dryRun bool) {
	legs := make([]api.Leg, 0, len(trades))
	for _, tr := range trades {
		legs = append(legs, api.Leg{OptionID: tr.OptionID, Contracts: tr.Contracts})
	}
	req := api.TradeRequest{Topics: []api.TopicTrade{{TopicID: t.topicID, Legs: legs}}}

	if dryRun {
		for _, tr := range trades {
			r := TradeRecord{
				Time:      time.Now(),
				Range:     tr.Range,
				Contracts: tr.Contracts,
				CostSats:  tr.YesPriceMillisats * int64(tr.Contracts) / 1000,
				DryRun:    true,
			}
			t.mu.Lock()
			t.st.tradelog = append(t.st.tradelog, r)
			t.st.totalCost += r.CostSats
			t.appendLogLocked(fmt.Sprintf("[DRY] range=%s contracts=%d cost=%dsats",
				tr.Range, tr.Contracts, r.CostSats))
			t.mu.Unlock()
		}
		return
	}

	resp, err := t.client.PlaceTrade(req)
	if err != nil {
		t.setErr("trade: " + err.Error())
		return
	}
	if resp.TopicsFailed > 0 {
		t.setErr(fmt.Sprintf("trade rejected (topics_failed=%d)", resp.TopicsFailed))
		return
	}
	for _, res := range resp.Results {
		r := TradeRecord{
			Time:     time.Now(),
			TradeID:  res.TradeID,
			CostSats: res.TotalCostMillisats / 1000,
		}
		if len(trades) > 0 {
			r.Range = trades[0].Range
			r.Contracts = trades[0].Contracts
		}
		t.mu.Lock()
		t.st.tradelog = append(t.st.tradelog, r)
		t.st.totalCost += r.CostSats
		t.appendLogLocked(fmt.Sprintf("trade placed: id=%s cost=%dsats", res.TradeID, r.CostSats))
		t.mu.Unlock()
	}
}

func (t *TUI) setErr(msg string) {
	t.mu.Lock()
	t.st.lastErr = msg
	t.appendLogLocked("[ERR] " + msg)
	t.mu.Unlock()
	t.render()
}

func (t *TUI) appendLog(msg string) {
	t.mu.Lock()
	t.appendLogLocked(msg)
	t.mu.Unlock()
}

func (t *TUI) appendLogLocked(msg string) {
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	t.st.logs = append(t.st.logs, entry)
	if len(t.st.logs) > 200 {
		t.st.logs = t.st.logs[len(t.st.logs)-200:]
	}
}

func (t *TUI) render() {
	t.mu.Lock()
	st := t.st
	logs := make([]string, len(st.logs))
	copy(logs, st.logs)
	t.mu.Unlock()

	w := termWidth()
	var b strings.Builder

	b.WriteString("\033[H") 

	statusStr := colorf(green, "LIVE")
	if !st.running {
		statusStr = colorf(yel, "PAUSED")
	}
	if st.dryRun {
		statusStr += " " + colorf(dim, "[DRY]")
	}
	expStr := ""
	if !st.expiry.IsZero() {
		d := time.Until(st.expiry).Round(time.Second)
		expStr = fmt.Sprintf("exp in %s", d)
	}
	fngColor := green
	if st.fng < 30 {
		fngColor = red
	} else if st.fng > 70 {
		fngColor = yel
	}
	header := fmt.Sprintf(" %sGLIMPSE KELLY BOT%s │ BTC $%s │ F&G %s │ %s │ %s ",
		bold, reset,
		colorf(white, fmt.Sprintf("%.2f", st.spot)),
		colorf(fngColor, fmt.Sprintf("%d %s", st.fng, st.fngLabel)),
		expStr,
		statusStr,
	)
	b.WriteString(colorf(bold+"\033[44m", pad(stripANSI(header)+" ", w)))
	b.WriteString(reset + "\n")

	// Sub-header: model name + balance + last update
	modelName := ""
	if t.forecaster != nil {
		modelName = t.forecaster.Name()
	}
	subHdr := fmt.Sprintf(" model: %s │ balance: %dsats │ session cost: %dsats │ updated: %s ",
		modelName, st.balance, st.totalCost, st.lastUpdate.Format("15:04:05"))
	b.WriteString(colorf(dim, pad(subHdr, w)) + "\n")
	b.WriteString(strings.Repeat("─", w) + "\n")

	if st.lastErr != "" {
		b.WriteString(colorf(red, " ✖ "+st.lastErr) + "\n")
	}

	leftW := w/2 - 1
	rightW := w - leftW - 3

	// Build left panel: histogram (market vs model probs, ±8 bins from spot)
	leftLines := buildHistogram(st.mktProbs, st.mdlProbs, st.spot, leftW)

	// Build right panel top: mispriced bins table
	rightTop := buildMispricedTable(st.trades, rightW)

	// Build right panel bottom: trade log
	rightBot := buildTradeLog(st.tradelog, rightW)

	// Merge left + right
	rightLines := append(rightTop, rightBot...)
	maxRows := len(leftLines)
	if len(rightLines) > maxRows {
		maxRows = len(rightLines)
	}

	for i := 0; i < maxRows; i++ {
		lLine := ""
		rLine := ""
		if i < len(leftLines) {
			lLine = leftLines[i]
		}
		if i < len(rightLines) {
			rLine = rightLines[i]
		}
		b.WriteString(pad(lLine, leftW+1))
		b.WriteString(colorf(dim, "│"))
		b.WriteString(" " + rLine + "\n")
	}

	b.WriteString(strings.Repeat("─", w) + "\n")

	logRows := 6
	b.WriteString(colorf(bold, " LOGS") + "\n")
	start := len(logs) - logRows
	if start < 0 {
		start = 0
	}
	for _, l := range logs[start:] {
		line := " " + l
		if len(line) > w {
			line = line[:w]
		}
		b.WriteString(colorf(dim, pad(line, w)) + "\n")
	}

	b.WriteString(strings.Repeat("─", w) + "\n")
	b.WriteString(colorf(dim, " [q]quit  [s]start/stop  [d]dry-run toggle  [r]force refresh") + "\n")

	fmt.Print(b.String())
}

// buildHistogram renders a horizontal bar chart showing ±8 bins around spot
func buildHistogram(mkt, mdl []float64, spot float64, width int) []string {
	if len(mkt) == 0 || len(mdl) == 0 {
		return []string{colorf(dim, " No market data yet")}
	}
	center := lmsr.SpotToBin(spot)
	lo := center - 8
	if lo < 0 {
		lo = 0
	}
	hi := center + 9
	if hi > lmsr.N {
		hi = lmsr.N
	}

	// Find max prob for scaling
	maxP := 0.0
	for i := lo; i < hi; i++ {
		if mkt[i] > maxP {
			maxP = mkt[i]
		}
		if mdl[i] > maxP {
			maxP = mdl[i]
		}
	}
	if maxP == 0 {
		maxP = 1
	}

	barWidth := width - 32 
	if barWidth < 4 {
		barWidth = 4
	}

	lines := []string{
		colorf(bold, fmt.Sprintf(" %-4s %-13s %5s %5s %s", "BIN", "RANGE", "MKT%", "MDL%", "BAR (M=market P=model)")),
	}
	for i := lo; i < hi; i++ {
		m := mkt[i]
		p := mdl[i]

		mBars := int(math.Round(m / maxP * float64(barWidth)))
		pBars := int(math.Round(p / maxP * float64(barWidth)))

		prefix := " "
		if i == center {
			prefix = colorf(yel, "→")
		}

		hi2 := lmsr.BinHigh(i)
		rangeStr := fmt.Sprintf("%g-%g", lmsr.BinLow(i), hi2)
		if math.IsInf(hi2, 1) {
			rangeStr = fmt.Sprintf("%g+", lmsr.BinLow(i))
		}

		// Bar: M chars of ▓ (market), then extra for model overlap in cyan
		mBar := strings.Repeat("▓", mBars)
		extra := pBars - mBars
		var pBar string
		if extra > 0 {
			pBar = colorf(cyan, strings.Repeat("▒", extra))
		}

		edgeMark := " "
		if p-m > 0.005 {
			edgeMark = colorf(green, "*")
		}

		line := fmt.Sprintf("%s%s %-4d %-13s %5.2f%% %5.2f%%%s %s%s",
			prefix, edgeMark, i,
			pad(rangeStr, 13),
			m*100, p*100,
			reset,
			mBar, pBar,
		)
		lines = append(lines, line)
	}
	return lines
}

// buildMispricedTable renders the Kelly-recommended trades
func buildMispricedTable(trades []kelly.Trade, width int) []string {
	hdr := colorf(bold, fmt.Sprintf(" %-13s %5s %5s %5s %5s %5s",
		"RANGE", "MKT%", "MDL%", "EDGE", "CTRS", "COST"))
	lines := []string{
		colorf(bold, " KELLY TRADES (mispriced bins)"),
		hdr,
		strings.Repeat("─", width),
	}
	if len(trades) == 0 {
		lines = append(lines, colorf(dim, " No underpriced bins found"))
		return lines
	}
	for _, tr := range trades {
		cost := tr.YesPriceMillisats * int64(tr.Contracts) / 1000
		edgeColor := green
		if tr.Edge < 0.01 {
			edgeColor = yel
		}
		line := fmt.Sprintf(" %-13s %5.2f%% %5.2f%%%s %5.2f%%%s %5d %5dsats",
			pad(tr.Range, 13),
			tr.MarketProb*100, tr.ModelProb*100, reset,
			tr.Edge*100, colorf(edgeColor, ""),
			tr.Contracts, cost,
		)
		lines = append(lines, line)
	}
	return lines
}

// buildTradeLog renders the most recent trades from this session
func buildTradeLog(log []TradeRecord, width int) []string {
	lines := []string{
		strings.Repeat("─", width),
		colorf(bold, " SESSION TRADE LOG"),
		colorf(dim, fmt.Sprintf(" %-8s %-13s %5s %8s", "TIME", "RANGE", "CTRS", "COST")),
	}
	start := len(log) - 5
	if start < 0 {
		start = 0
	}
	for _, r := range log[start:] {
		tag := ""
		if r.DryRun {
			tag = colorf(dim, " [dry]")
		}
		line := fmt.Sprintf(" %-8s %-13s %5d %6dsats%s",
			r.Time.Format("15:04:05"),
			pad(r.Range, 13),
			r.Contracts, r.CostSats, tag,
		)
		lines = append(lines, line)
	}
	if len(log) == 0 {
		lines = append(lines, colorf(dim, " No trades yet"))
	}
	return lines
}

// termWidth returns the terminal width (defaults to 120)
func termWidth() int {
	// Try to detect via ioctl; fall back to 120
	type winsize struct{ Row, Col, Xpixel, Ypixel uint16 }
	// Simple heuristic: check $COLUMNS env first
	if cols := os.Getenv("COLUMNS"); cols != "" {
		n := 0
		fmt.Sscanf(cols, "%d", &n)
		if n > 40 {
			return n
		}
	}
	return 120
}

// stripANSI removes ANSI escape codes for length calculation
func stripANSI(s string) string {
	out := strings.Builder{}
	inEsc := false
	for _, c := range s {
		if c == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				inEsc = false
			}
			continue
		}
		out.WriteRune(c)
	}
	return out.String()
}
