package model

import (
	"math"
	"testing"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

func TestGaussianSumsToOne(t *testing.T) {
	m := NewGaussianModel(5.0)
	probs, err := m.Forecast(80000, 50)
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
	probs, _ := m.Forecast(spot, 50)
	peakBin := lmsr.SpotToBin(spot)

	for i, p := range probs {
		if i != peakBin && p > probs[peakBin] {
			t.Errorf("bin %d (p=%.4f) > spot bin %d (p=%.4f)", i, p, peakBin, probs[peakBin])
		}
	}
}

func TestUniformModel(t *testing.T) {
	m := &UniformModel{}
	probs, _ := m.Forecast(0, 0)
	want := 1.0 / float64(lmsr.N)
	for i, p := range probs {
		if math.Abs(p-want) > 1e-12 {
			t.Errorf("probs[%d] = %v, want %v", i, p, want)
			break
		}
	}
}

func TestSkewedGaussianContrarian(t *testing.T) {
	m := NewSkewedGaussianModel(5.0, 2.0)
	spot := 80000.0

	fearProbs, _ := m.Forecast(spot, 10)  // extreme fear -> bullish shift
	greedProbs, _ := m.Forecast(spot, 90) // extreme greed -> bearish shift

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
