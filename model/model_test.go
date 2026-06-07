package model

import (
	"math"
	"testing"
	"time"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

func TestGaussianSumsToOne(t *testing.T) {
	m := NewGaussianModel(5.0)
	probs, err := m.Forecast(ForecastContext{SpotPrice: 80000, FearGreedIndex: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != lmsr.N {
		t.Fatalf("len=%d want %d", len(probs), lmsr.N)
	}
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.12f, want 1.0", sum)
	}
}

func TestGaussianPeakAtSpot(t *testing.T) {
	m := NewGaussianModel(5.0)
	spot := 80000.0
	probs, _ := m.Forecast(ForecastContext{SpotPrice: spot, FearGreedIndex: 50})
	peakBin := lmsr.SpotToBin(spot)

	for i, p := range probs {
		if i != peakBin && p > probs[peakBin] {
			t.Errorf("bin %d (p=%.4f) > spot bin %d (p=%.4f)", i, p, peakBin, probs[peakBin])
		}
	}
}

func TestUniformModel(t *testing.T) {
	m := &UniformModel{}
	probs, _ := m.Forecast(ForecastContext{})
	want := 1.0 / float64(lmsr.N)
	for i, p := range probs {
		if math.Abs(p-want) > 1e-12 {
			t.Errorf("probs[%d] = %v, want %v", i, p, want)
			break
		}
	}
}

func TestLogNormalSumsToOne(t *testing.T) {
	m := NewLogNormalModel(0.65, 6.0/8760.0)
	probs, err := m.Forecast(ForecastContext{SpotPrice: 80000})
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != lmsr.N {
		t.Fatalf("len=%d want %d", len(probs), lmsr.N)
	}
	sum := 0.0
	for _, p := range probs {
		sum += p
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("sum = %.12f, want 1.0", sum)
	}
}

func TestLogNormalRightSkew(t *testing.T) {
	// For a log-normal: mode < median < mean = spot (Itô drift correction).
	// So the peak (mode) bin must be strictly below the spot bin.
	m := NewLogNormalModel(0.65, 6.0/8760.0)
	spot := 80000.0
	probs, _ := m.Forecast(ForecastContext{SpotPrice: spot})

	peakBin := 0
	for i, p := range probs {
		if p > probs[peakBin] {
			peakBin = i
		}
	}
	spotBin := lmsr.SpotToBin(spot)
	if peakBin >= spotBin {
		t.Errorf("log-normal mode bin (%d) should be < spot bin (%d) due to right skew", peakBin, spotBin)
	}
}

func TestLogNormalUsesContextVol(t *testing.T) {
	spot := 80000.0
	// High-vol model should spread probability more widely than low-vol.
	lo := NewLogNormalModel(0.20, 6.0/8760.0)
	hi := NewLogNormalModel(0.90, 6.0/8760.0)

	loProbs, _ := lo.Forecast(ForecastContext{SpotPrice: spot})
	hiProbs, _ := hi.Forecast(ForecastContext{SpotPrice: spot})

	spotBin := lmsr.SpotToBin(spot)
	if loProbs[spotBin] <= hiProbs[spotBin] {
		t.Errorf("low-vol peak (%.4f) should exceed high-vol peak (%.4f)", loProbs[spotBin], hiProbs[spotBin])
	}
}

func TestEnsembleBlend(t *testing.T) {
	// 50% gaussian + 50% uniform should give probs strictly between the two
	gauss := NewGaussianModel(5.0)
	unif := &UniformModel{}
	ens, err := NewEnsembleForecaster([]Forecaster{gauss, unif}, []float64{0.5, 0.5})
	if err != nil {
		t.Fatal(err)
	}

	ctx := ForecastContext{SpotPrice: 80000, FearGreedIndex: 50}
	gProbs, _ := gauss.Forecast(ctx)
	uProbs, _ := unif.Forecast(ctx)
	eProbs, err := ens.Forecast(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(eProbs) != lmsr.N {
		t.Fatalf("ensemble len=%d want %d", len(eProbs), lmsr.N)
	}

	// Sum must equal 1
	sum := 0.0
	for _, p := range eProbs {
		sum += p
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("ensemble sum=%.12f, want 1.0", sum)
	}

	// Each ensemble prob must lie between the two component probs
	spotBin := lmsr.SpotToBin(80000)
	ep := eProbs[spotBin]
	lo, hi := uProbs[spotBin], gProbs[spotBin]
	if lo > hi {
		lo, hi = hi, lo
	}
	if ep < lo-1e-12 || ep > hi+1e-12 {
		t.Errorf("ensemble prob at spot bin (%.6f) not in [%.6f, %.6f]", ep, lo, hi)
	}
}

func TestEnsembleBadWeights(t *testing.T) {
	g := NewGaussianModel(5.0)
	_, err := NewEnsembleForecaster([]Forecaster{g, g}, []float64{0.6, 0.6})
	if err == nil {
		t.Error("expected error for weights summing to 1.2")
	}
	_, err = NewEnsembleForecaster([]Forecaster{g}, []float64{0.5, 0.5})
	if err == nil {
		t.Error("expected error for mismatched lengths")
	}
}

func TestHTTPForecasterName(t *testing.T) {
	m := NewHTTPForecaster("http://localhost:9000/forecast", 0)
	if m.Name() != "http(http://localhost:9000/forecast)" {
		t.Errorf("unexpected Name: %s", m.Name())
	}
	if m.Timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", m.Timeout)
	}
}

func TestFromNameHTTP(t *testing.T) {
	f, err := FromName("http://localhost:9000/forecast", 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.(*HTTPForecaster); !ok {
		t.Errorf("expected *HTTPForecaster, got %T", f)
	}
}

func TestFromNameLognormal(t *testing.T) {
	f, err := FromName("lognormal", 5.0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.(*LogNormalModel); !ok {
		t.Errorf("expected *LogNormalModel, got %T", f)
	}
}

func TestSkewedGaussianContrarian(t *testing.T) {
	m := NewSkewedGaussianModel(5.0, 2.0)
	spot := 80000.0

	fearProbs, _ := m.Forecast(ForecastContext{SpotPrice: spot, FearGreedIndex: 10})
	greedProbs, _ := m.Forecast(ForecastContext{SpotPrice: spot, FearGreedIndex: 90})

	// Fear distribution should have higher mean bin than greed distribution
	fearMean := 0.0
	greedMean := 0.0
	for i, p := range fearProbs {
		fearMean += float64(i) * p
		greedMean += float64(i) * greedProbs[i]
	}
	if fearMean <= greedMean {
		t.Errorf("fear mean bin (%.2f) should be > greed mean bin (%.2f)", fearMean, greedMean)
	}
}
