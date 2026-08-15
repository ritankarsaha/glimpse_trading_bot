package model

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// skewedMarketProbs returns lmsr.ImpliedProbs for a quantity vector where bin
// `peak` has been bought up — i.e. a market that's pricing `peak` higher than
// the uniform-seed baseline.
func skewedMarketProbs(peak int) []float64 {
	q := make([]float64, lmsr.N)
	for i := range q {
		q[i] = lmsr.Q0
	}
	q[peak] = lmsr.Q0 * 5
	return lmsr.ImpliedProbs(q)
}

func sumProbs(p []float64) float64 {
	s := 0.0
	for _, v := range p {
		s += v
	}
	return s
}

func TestCombineLogOddsNilImpliedProbsPassthrough(t *testing.T) {
	fundamental := NewGaussianModel(5.0)
	m := NewLogitCombinerModel(fundamental, 1.0, 1.0)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50} // ImpliedProbs nil

	got, err := m.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := fundamental.Forecast(ctx)
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-12 {
			t.Fatalf("bin %d: got %.12f want %.12f (expected passthrough)", i, got[i], want[i])
		}
	}
}

func TestCombineLogOddsPureFundamental(t *testing.T) {
	fundamental := NewGaussianModel(5.0)
	m := NewLogitCombinerModel(fundamental, 1.0, 0.0)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50, ImpliedProbs: skewedMarketProbs(450)}

	got, err := m.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := fundamental.Forecast(ctx)
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("bin %d: got %.12f want %.12f (betaM=0 should be pure fundamental)", i, got[i], want[i])
		}
	}
}

func TestCombineLogOddsPureMarket(t *testing.T) {
	fundamental := NewGaussianModel(5.0)
	m := NewLogitCombinerModel(fundamental, 0.0, 1.0)
	pm := skewedMarketProbs(450)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50, ImpliedProbs: pm}

	got, err := m.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pmSum := sumProbs(pm)
	for i := range got {
		want := pm[i] / pmSum
		if math.Abs(got[i]-want) > 1e-9 {
			t.Fatalf("bin %d: got %.12f want %.12f (betaF=0 should be renormalized market)", i, got[i], want)
		}
	}
}

func TestCombineLogOddsSumsToOne(t *testing.T) {
	fundamental := NewGaussianModel(5.0)
	m := NewLogitCombinerModel(fundamental, 1.0, 1.0)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50, ImpliedProbs: skewedMarketProbs(450)}

	got, err := m.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sumProbs(got); math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.12f, want 1.0", sum)
	}
}

func TestCombineLogOddsShiftsTowardMarket(t *testing.T) {
	// Gaussian fundamental peaks at the spot bin (400 for spot=80000).
	// The market signal is skewed toward bin 450. Increasing betaM should
	// pull probability mass toward bin 450.
	fundamental := NewGaussianModel(5.0)
	pm := skewedMarketProbs(450)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50, ImpliedProbs: pm}

	low := NewLogitCombinerModel(fundamental, 1.0, 0.2)
	high := NewLogitCombinerModel(fundamental, 1.0, 2.0)

	lowProbs, err := low.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}
	highProbs, err := high.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if highProbs[450] <= lowProbs[450] {
		t.Errorf("higher betaM should shift more mass to the market's preferred bin: low=%.6f high=%.6f",
			lowProbs[450], highProbs[450])
	}
}

func TestFundamentalProbs(t *testing.T) {
	fundamental := NewGaussianModel(5.0)
	combiner := NewLogitCombinerModel(fundamental, 1.0, 1.0)
	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50, ImpliedProbs: skewedMarketProbs(450)}

	want, err := fundamental.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}

	fromCombiner, err := FundamentalProbs(combiner, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if math.Abs(fromCombiner[i]-want[i]) > 1e-12 {
			t.Fatalf("bin %d: combiner fundamental probs diverge: got %.12f want %.12f", i, fromCombiner[i], want[i])
		}
	}

	fromPlain, err := FundamentalProbs(fundamental, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if math.Abs(fromPlain[i]-want[i]) > 1e-12 {
			t.Fatalf("bin %d: plain forecaster probs diverge: got %.12f want %.12f", i, fromPlain[i], want[i])
		}
	}
}

func TestFromNameBenterDefaults(t *testing.T) {
	f, err := FromName("benter", 5.0)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := f.(*LogitCombinerModel)
	if !ok {
		t.Fatalf("expected *LogitCombinerModel, got %T", f)
	}
	if _, ok := c.Fundamental.(*LogNormalModel); !ok {
		t.Errorf("expected default fundamental *LogNormalModel, got %T", c.Fundamental)
	}
	if c.BetaFundamental != 1.0 || c.BetaMarket != 1.0 {
		t.Errorf("expected default betas 1.0/1.0, got %.2f/%.2f", c.BetaFundamental, c.BetaMarket)
	}
}

func TestFromNameBenterWithInner(t *testing.T) {
	f, err := FromName("benter:gaussian", 5.0)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := f.(*LogitCombinerModel)
	if !ok {
		t.Fatalf("expected *LogitCombinerModel, got %T", f)
	}
	if _, ok := c.Fundamental.(*GaussianModel); !ok {
		t.Errorf("expected inner *GaussianModel, got %T", c.Fundamental)
	}
}

func TestFromNameWithCalibrationFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calibration.json")
	weights := CalibrationWeights{BetaFundamental: 1.5, BetaMarket: 0.5}
	data, err := json.Marshal(weights)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	f, err := FromNameWithCalibration("benter", 5.0, path)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := f.(*LogitCombinerModel)
	if !ok {
		t.Fatalf("expected *LogitCombinerModel, got %T", f)
	}
	if c.BetaFundamental != 1.5 || c.BetaMarket != 0.5 {
		t.Errorf("got betas %.2f/%.2f, want 1.5/0.5", c.BetaFundamental, c.BetaMarket)
	}
}

func TestFromNameWithCalibrationMissingFile(t *testing.T) {
	f, err := FromNameWithCalibration("benter", 5.0, filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := f.(*LogitCombinerModel)
	if c.BetaFundamental != 1.0 || c.BetaMarket != 1.0 {
		t.Errorf("missing calibration file should fall back to 1.0/1.0, got %.2f/%.2f", c.BetaFundamental, c.BetaMarket)
	}
}
