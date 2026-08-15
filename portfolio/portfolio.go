package portfolio

import (
	"fmt"
	"math"

	"github.com/ritankarsaha/glimpse-bot/kelly"
)

type Config struct {
	KellyCfg        kelly.Config
	ThemeCapPct     float64 // fraction of budgetSats allowed across all markets combined; <=0 disables
	PerMarketCapPct float64 // fraction of budgetSats allowed in any single market; <=0 disables
}

func DefaultConfig() Config {
	return Config{
		KellyCfg:        kelly.DefaultConfig(),
		ThemeCapPct:     0.25,
		PerMarketCapPct: 0.10,
	}
}

// MarketInput is one open market's state, ready for Kelly optimization.
type MarketInput struct {
	TopicID    int
	OutcomeMap *kelly.OutcomeMap
	ModelProbs []float64
}

type allocation struct {
	topicID int
	trades  []kelly.Trade
	cost    int64
	q       []float64
	tau     float64
}

// scale rescales Contracts by factor (floor), dropping legs that round to
// zero, and recomputes cost.
func (a *allocation) scale(factor float64) {
	trades := a.trades[:0:0]
	for _, t := range a.trades {
		c := int(math.Floor(float64(t.Contracts) * factor))
		if c < 1 {
			continue
		}
		t.Contracts = c
		trades = append(trades, t)
	}
	a.trades = trades
	a.cost = kelly.ExpectedCost(a.q, trades, a.tau)
}

func Allocate(inputs []MarketInput, budgetSats float64, cfg Config) (map[int][]kelly.Trade, error) {
	if budgetSats <= 0 {
		return nil, nil
	}

	var allocs []allocation
	var total int64
	for _, in := range inputs {
		trades, err := kelly.Optimize(in.OutcomeMap, in.ModelProbs, budgetSats, cfg.KellyCfg)
		if err != nil {
			return nil, fmt.Errorf("topic %d: %w", in.TopicID, err)
		}
		if len(trades) == 0 {
			continue
		}
		cost := kelly.ExpectedCost(in.OutcomeMap.Q, trades, cfg.KellyCfg.Tau)
		allocs = append(allocs, allocation{topicID: in.TopicID, trades: trades, cost: cost, q: in.OutcomeMap.Q, tau: cfg.KellyCfg.Tau})
		total += cost
	}
	if len(allocs) == 0 {
		return nil, nil
	}

	if cfg.ThemeCapPct > 0 {
		themeBudget := budgetSats * cfg.ThemeCapPct
		if float64(total) > themeBudget {
			scaleFactor := themeBudget / float64(total)
			for i := range allocs {
				allocs[i].scale(scaleFactor)
			}
		}
	}

	if cfg.PerMarketCapPct > 0 {
		perMarketBudget := budgetSats * cfg.PerMarketCapPct
		for i := range allocs {
			if float64(allocs[i].cost) > perMarketBudget {
				allocs[i].scale(perMarketBudget / float64(allocs[i].cost))
			}
		}
	}

	out := make(map[int][]kelly.Trade)
	for _, a := range allocs {
		if len(a.trades) > 0 {
			out[a.topicID] = a.trades
		}
	}
	return out, nil
}
