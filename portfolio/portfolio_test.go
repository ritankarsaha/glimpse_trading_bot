package portfolio

import (
	"fmt"
	"testing"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/kelly"
	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// buildSingleBinMarket constructs an OutcomeMap with a single tradeable bin
// (peakBin) and a model that assigns it peakProb (with the remainder spread
// uniformly over the rest), so kelly.Optimize finds exactly one underpriced
// bin.
func buildSingleBinMarket(peakBin int, peakProb float64, yesPriceMillisats int64) (*kelly.OutcomeMap, []float64) {
	lo := int64(peakBin) * int64(lmsr.BinWidth)
	hi := lo + int64(lmsr.BinWidth)
	outcomes := []api.Outcome{
		{OptionID: peakBin + 1, Name: fmt.Sprintf("%d-%d", lo, hi), YesPriceMillisats: yesPriceMillisats},
	}
	om := kelly.BuildOutcomeMap(outcomes)
	probs := make([]float64, lmsr.N)
	rest := (1 - peakProb) / float64(lmsr.N-1)
	for i := range probs {
		probs[i] = rest
	}
	probs[peakBin] = peakProb
	return om, probs
}

func TestAllocateNoBudgetOrNoTrades(t *testing.T) {
	om, probs := buildSingleBinMarket(400, 0.5, 200000)
	inputs := []MarketInput{{TopicID: 1, OutcomeMap: om, ModelProbs: probs}}

	if out, err := Allocate(inputs, 0, DefaultConfig()); err != nil || out != nil {
		t.Errorf("zero budget: got out=%v err=%v, want nil,nil", out, err)
	}

	// A model that agrees with the market everywhere has no edge anywhere.
	uniform := make([]float64, lmsr.N)
	for i := range uniform {
		uniform[i] = 1.0 / lmsr.N
	}
	noEdgeOM := kelly.BuildOutcomeMap(nil)
	out, err := Allocate([]MarketInput{{TopicID: 1, OutcomeMap: noEdgeOM, ModelProbs: uniform}}, 100000, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected no allocation when no market has an edge, got %v", out)
	}
}

func TestAllocateThemeCapScalesProportionally(t *testing.T) {
	// TruthfulCap bounds any single bin's position on its own (it's a real
	// protection: you can't push a bin's normalized price past your own
	// believed probability), so even a maxed-out single-bin position costs
	// only ~195 sats of the true convex LMSR cost — nowhere near a
	// percentage-of-100k-sat-wallet cap unless that percentage is itself
	// tiny. peakProb=0.99 (near-certain belief) pushes each market's
	// unconstrained position to its truthful cap: 228 contracts / 195 sats.
	om1, probs1 := buildSingleBinMarket(400, 0.99, 200000)
	om2, probs2 := buildSingleBinMarket(410, 0.99, 200000)
	inputs := []MarketInput{
		{TopicID: 1, OutcomeMap: om1, ModelProbs: probs1},
		{TopicID: 2, OutcomeMap: om2, ModelProbs: probs2},
	}

	const budget = 100000.0
	cfg := DefaultConfig()
	cfg.ThemeCapPct = 0.002 // 200 sats — below the unconstrained combined cost of ~390 sats
	cfg.PerMarketCapPct = 1.0 // effectively disabled for this test

	out, err := Allocate(inputs, budget, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected both markets to retain trades, got %d", len(out))
	}

	var total int64
	for topicID, trades := range out {
		q := om1.Q
		if topicID == 2 {
			q = om2.Q
		}
		cost := kelly.ExpectedCost(q, trades, cfg.KellyCfg.Tau)
		total += cost
		t.Logf("topic %d: %+v cost=%d", topicID, trades, cost)
	}

	themeBudget := int64(budget * cfg.ThemeCapPct)
	if total > themeBudget {
		t.Errorf("combined cost %d exceeds theme budget %d", total, themeBudget)
	}

	// Both markets are symmetric (same edge, same price) so they should be
	// scaled down to the same contract count.
	c1 := out[1][0].Contracts
	c2 := out[2][0].Contracts
	if c1 != c2 {
		t.Errorf("symmetric markets scaled unevenly: topic1=%d topic2=%d", c1, c2)
	}
	// Sanity: scaling actually happened (unconstrained size is 228 contracts each).
	if c1 >= 228 {
		t.Errorf("expected contracts to be scaled down from the unconstrained 228, got %d", c1)
	}
}

func TestAllocatePerMarketCapAppliesIndividually(t *testing.T) {
	// topic 1: near-certain belief (peakProb=0.99) maxes out its truthful
	// cap at 228 contracts / 195 sats — enough to breach a 150-sat
	// per-market cap on its own, but not a (generous) theme cap.
	om1, probs1 := buildSingleBinMarket(400, 0.99, 200000)
	// topic 2: more modest belief (peakProb=0.5), truthful-capped at 138
	// contracts / 100 sats — under the 150-sat per-market cap.
	om2, probs2 := buildSingleBinMarket(410, 0.5, 200000)
	inputs := []MarketInput{
		{TopicID: 1, OutcomeMap: om1, ModelProbs: probs1},
		{TopicID: 2, OutcomeMap: om2, ModelProbs: probs2},
	}

	const budget = 100000.0
	cfg := DefaultConfig()
	cfg.ThemeCapPct = 1.0 // effectively disabled
	cfg.PerMarketCapPct = 0.0015 // 150 sats

	out, err := Allocate(inputs, budget, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected both markets to retain trades, got %d", len(out))
	}

	perMarketBudget := int64(budget * cfg.PerMarketCapPct)
	cost1 := kelly.ExpectedCost(om1.Q, out[1], cfg.KellyCfg.Tau)
	cost2 := kelly.ExpectedCost(om2.Q, out[2], cfg.KellyCfg.Tau)
	t.Logf("topic1 cost=%d topic2 cost=%d perMarketBudget=%d", cost1, cost2, perMarketBudget)

	if cost1 > perMarketBudget {
		t.Errorf("topic1 cost %d exceeds per-market budget %d", cost1, perMarketBudget)
	}
	if cost2 > perMarketBudget {
		t.Errorf("topic2 cost %d exceeds per-market budget %d", cost2, perMarketBudget)
	}
	// topic2 was already under the cap and shouldn't be touched.
	if out[2][0].Contracts != 138 {
		t.Errorf("topic2 should be unscaled (138 contracts), got %d", out[2][0].Contracts)
	}
	// topic1 must actually have been scaled down from its unconstrained size.
	if out[1][0].Contracts >= 228 {
		t.Errorf("topic1 should be scaled down from 228, got %d", out[1][0].Contracts)
	}
}
