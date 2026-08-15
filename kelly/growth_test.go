package kelly

import (
	"math"
	"testing"
)

func TestKellyFraction(t *testing.T) {
	got := KellyFraction(0.6, 0.5)
	want := 0.6 - 0.4*0.5/0.5 // 0.2
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("KellyFraction(0.6,0.5) = %.6f, want %.6f", got, want)
	}
}

func TestKellyFractionNoEdgeIsZero(t *testing.T) {
	if got := KellyFraction(0.5, 0.5); got != 0 {
		t.Errorf("KellyFraction(0.5,0.5) = %.6f, want 0 (no edge)", got)
	}
	// Negative edge should also clamp to zero, not go negative.
	if got := KellyFraction(0.3, 0.5); got != 0 {
		t.Errorf("KellyFraction(0.3,0.5) = %.6f, want 0 (negative edge)", got)
	}
}

func TestKellyFractionInvalidPrice(t *testing.T) {
	if got := KellyFraction(0.6, 0); got != 0 {
		t.Errorf("KellyFraction(0.6,0) = %.6f, want 0", got)
	}
	if got := KellyFraction(0.6, 1); got != 0 {
		t.Errorf("KellyFraction(0.6,1) = %.6f, want 0", got)
	}
}

func TestExpectedLogGrowthZeroStakeIsZero(t *testing.T) {
	if got := ExpectedLogGrowth(0.5, 0.7, 0); got != 0 {
		t.Errorf("ExpectedLogGrowth(.,.,0) = %.6f, want 0", got)
	}
}

func TestExpectedLogGrowthPositiveAtKellyOptimum(t *testing.T) {
	// At the true probability, betting the full-Kelly fraction must give
	// strictly positive expected log-growth when there's an edge.
	c, p := 0.5, 0.7
	f := KellyFraction(p, c)
	growth := ExpectedLogGrowth(c, p, f)
	if growth <= 0 {
		t.Errorf("expected positive growth at Kelly optimum, got %.6f", growth)
	}
}

func TestOverestimationImpact_FullKellyCanShrinkCapital(t *testing.T) {
	// Trader believes p=0.9 on a market priced at c=0.5 (huge perceived
	// edge), bets full Kelly, but the true probability is only halfway
	// between c and the perceived p (overestimateFactor=2 -> pTrue=0.7).
	// The bet is sized for the 0.4 perceived edge but only a 0.2 true edge
	// exists, so it's oversized -> capital shrinkage (negative growth).
	growthIfCorrect, growthTrue := OverestimationImpact(0.5, 0.9, 1.0, 2.0)

	if growthIfCorrect <= 0 {
		t.Errorf("growthIfCorrect should be positive (the perceived bet looks great): got %.6f", growthIfCorrect)
	}
	if growthTrue >= growthIfCorrect {
		t.Errorf("overestimation should reduce realized growth: growthTrue=%.6f growthIfCorrect=%.6f", growthTrue, growthIfCorrect)
	}
	if growthTrue >= 0 {
		t.Errorf("a 2x-overestimated full-Kelly bet should shrink capital (negative growth), got %.6f", growthTrue)
	}
}

func TestOverestimationImpact_FractionalKellyMitigates(t *testing.T) {
	// Same scenario as above, but quarter-Kelly. The smaller stake should
	// keep realized growth positive despite the overestimated edge.
	const kellyFraction = 0.25
	_, growthTrue := OverestimationImpact(0.5, 0.9, kellyFraction, 2.0)

	if growthTrue <= 0 {
		t.Errorf("quarter-Kelly should keep growth positive under 2x overestimation, got %.6f", growthTrue)
	}
}

func TestOverestimationImpact_NoOverestimationMatches(t *testing.T) {
	// overestimateFactor=1 means the trader's belief was correct -> both
	// growth figures should be identical.
	growthIfCorrect, growthTrue := OverestimationImpact(0.5, 0.7, 0.25, 1.0)
	if math.Abs(growthIfCorrect-growthTrue) > 1e-12 {
		t.Errorf("overestimateFactor=1 should leave growth unchanged: %.6f vs %.6f", growthIfCorrect, growthTrue)
	}
}
