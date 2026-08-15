package kelly

import (
	"fmt"
	"math"
	"testing"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// uniformOutcomeMap builds an OutcomeMap seeded at lmsr.Q0 everywhere (a
// freshly-opened market) with every bin present in BinToOutcome, so
// kelly.Optimize considers all N bins as candidates.
func uniformOutcomeMap() *OutcomeMap {
	outcomes := make([]api.Outcome, lmsr.N)
	for i := range outcomes {
		lo := int64(i) * int64(lmsr.BinWidth)
		hi := lo + int64(lmsr.BinWidth)
		outcomes[i] = api.Outcome{OptionID: i + 1, Name: fmt.Sprintf("%d-%d", lo, hi)}
	}
	return BuildOutcomeMap(outcomes)
}

// TestOptimizeMarketProbUsesNormalizedPrice is a regression test for the
// LS-LMSR probability-scale bug: Glimpse's own price function sums to
// ~100*(V+1) at a fresh/uniform market (not 100), so the market-implied
// probability for a bin must be price/Σprices, not price/100. Dividing by a
// flat 100 overstates the market probability ~3x at this baseline state.
func TestOptimizeMarketProbUsesNormalizedPrice(t *testing.T) {
	om := uniformOutcomeMap()
	fair := 1.0 / float64(lmsr.N)
	modelProbs := make([]float64, lmsr.N)
	for i := range modelProbs {
		modelProbs[i] = fair
	}
	// Give bin 250 a clear, decisive edge over the fair 0.2% baseline: 5%.
	// Under the old (buggy) mktP=price/100≈0.6%, the reported MarketProb and
	// Edge would be wrong by ~3x (0.6% and 4.4% respectively) even though
	// this edge is large enough to clear MinEdge either way — the point of
	// this test is that the *reported values* are correct, which is what
	// downstream position sizing and logging depend on.
	modelProbs[250] = 0.05

	cfg := DefaultConfig()
	trades, err := Optimize(om, modelProbs, 100000, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 || trades[0].BinIndex != 250 {
		t.Fatalf("expected exactly one trade on bin 250, got %+v", trades)
	}

	tr := trades[0]
	if math.Abs(tr.MarketProb-fair) > 1e-6 {
		t.Errorf("MarketProb = %.6f, want ~%.6f (1/N, normalized) — got the old /100 scale instead?", tr.MarketProb, fair)
	}
	wantEdge := 0.05 - fair
	if math.Abs(tr.Edge-wantEdge) > 1e-6 {
		t.Errorf("Edge = %.6f, want ~%.6f", tr.Edge, wantEdge)
	}
	if tr.Edge <= cfg.MinEdge {
		t.Errorf("Edge %.6f should clear MinEdge %.6f now that mktP is normalized correctly", tr.Edge, cfg.MinEdge)
	}
}

// TestOptimizeSkipsFairlyPricedMarket checks the flip side: when the model
// agrees with the market's true (normalized) probability everywhere, there
// should be no trades — confirming the normalization fix didn't turn the
// edge filter into a no-op in the other direction.
func TestOptimizeSkipsFairlyPricedMarket(t *testing.T) {
	om := uniformOutcomeMap()
	fair := 1.0 / float64(lmsr.N)
	modelProbs := make([]float64, lmsr.N)
	for i := range modelProbs {
		modelProbs[i] = fair
	}

	trades, err := Optimize(om, modelProbs, 1000000, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 0 {
		t.Errorf("expected no trades when model matches the fair market price, got %d", len(trades))
	}
}

// TestExpectedCostIsConvexNotLinear is a regression test for the linear-cost
// approximation bug: LMSR cost is strictly convex, so buying 2x the
// contracts on a bin must cost strictly more than 2x the cost of buying 1x
// (the marginal price rises as you buy). A naive price*quantity estimate
// would be exactly linear (2x cost for 2x contracts).
func TestExpectedCostIsConvexNotLinear(t *testing.T) {
	om := uniformOutcomeMap()
	small := []Trade{{BinIndex: 250, Contracts: 100}}
	large := []Trade{{BinIndex: 250, Contracts: 200}}

	costSmall := ExpectedCost(om.Q, small, lmsr.Tau)
	costLarge := ExpectedCost(om.Q, large, lmsr.Tau)

	if costLarge <= 2*costSmall {
		t.Errorf("cost(200 contracts)=%d should be > 2*cost(100 contracts)=%d (convexity)", costLarge, 2*costSmall)
	}
}

// TestExpectedCostEmpty confirms the zero-trade edge case doesn't panic and
// returns zero cost.
func TestExpectedCostEmpty(t *testing.T) {
	om := uniformOutcomeMap()
	if cost := ExpectedCost(om.Q, nil, lmsr.Tau); cost != 0 {
		t.Errorf("ExpectedCost(nil trades) = %d, want 0", cost)
	}
}
