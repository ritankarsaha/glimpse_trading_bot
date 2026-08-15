package backtest

import (
	"math"
	"testing"
	"time"
)

func TestLogLikelihoodAndPseudoR2(t *testing.T) {
	probs := [][]float64{{0.5, 0.5}, {0.25, 0.75}}
	outcomes := []int{0, 1}

	ll := LogLikelihood(probs, outcomes)
	want := math.Log(0.5) + math.Log(0.75)
	if math.Abs(ll-want) > 1e-12 {
		t.Errorf("LogLikelihood = %.6f, want %.6f", ll, want)
	}

	nullLL := NullLogLikelihood(2, 2)
	wantNull := 2 * math.Log(0.5)
	if math.Abs(nullLL-wantNull) > 1e-12 {
		t.Errorf("NullLogLikelihood = %.6f, want %.6f", nullLL, wantNull)
	}

	r2 := PseudoR2(ll, nullLL)
	wantR2 := 1 - ll/nullLL
	if math.Abs(r2-wantR2) > 1e-12 {
		t.Errorf("PseudoR2 = %.6f, want %.6f", r2, wantR2)
	}
}

// peaked returns a length-n probability vector with peakProb at bin `peak`
// and the remainder spread uniformly over the other bins.
func peaked(n, peak int, peakProb float64) []float64 {
	out := make([]float64, n)
	rest := (1 - peakProb) / float64(n-1)
	for i := range out {
		out[i] = rest
	}
	out[peak] = peakProb
	return out
}

func TestFitLogitWeightsTooFewSamples(t *testing.T) {
	samples := []Sample{
		{Timestamp: time.Now(), TopicID: 1, FundamentalProbs: peaked(10, 0, 0.5), MarketProbs: peaked(10, 0, 0.5), OutcomeBin: 0},
	}
	_, err := FitLogitWeights(samples, 0.2)
	if err == nil {
		t.Error("expected error for too few resolved samples")
	}
}

func TestFitLogitWeightsInformativeFundamentalImprovesOverMarket(t *testing.T) {
	const n = 10
	fundamental := peaked(n, 0, 0.85)
	market := peaked(n, 5, 0.85)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var samples []Sample
	for i := 0; i < 100; i++ {
		outcome := 0
		if i%6 == 0 {
			outcome = 5
		}
		samples = append(samples, Sample{
			Timestamp:        base.Add(time.Duration(i) * time.Hour),
			TopicID:          1116,
			FundamentalProbs: fundamental,
			MarketProbs:      market,
			OutcomeBin:       outcome,
		})
	}

	report, err := FitLogitWeights(samples, 0.2)
	if err != nil {
		t.Fatal(err)
	}

	if report.NSamples != 100 {
		t.Errorf("NSamples = %d, want 100", report.NSamples)
	}
	if report.NTrain+report.NHoldout != report.NSamples {
		t.Errorf("NTrain(%d) + NHoldout(%d) != NSamples(%d)", report.NTrain, report.NHoldout, report.NSamples)
	}
	if report.DeltaR2 <= 0 {
		t.Errorf("expected DeltaR2 > 0 (fundamental should improve on a misleading market), got %.4f (R2Market=%.4f R2Combined=%.4f)",
			report.DeltaR2, report.R2Market, report.R2Combined)
	}
	if report.BetaFundamental < report.BetaMarket {
		t.Errorf("expected fitted weight to favor the informative fundamental: betaF=%.2f betaM=%.2f",
			report.BetaFundamental, report.BetaMarket)
	}
}

func TestFitLogitWeightsNoisyFundamentalDoesNotHurt(t *testing.T) {
	const n = 10
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var samples []Sample
	for i := 0; i < 100; i++ {
		outcome := i % n
	
		noisePeak := (i*7 + 3) % n
		samples = append(samples, Sample{
			Timestamp:        base.Add(time.Duration(i) * time.Hour),
			TopicID:          1116,
			FundamentalProbs: peaked(n, noisePeak, 0.85),
			MarketProbs:      peaked(n, outcome, 0.85),
			OutcomeBin:       outcome,
		})
	}

	report, err := FitLogitWeights(samples, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if report.R2Market < 0.5 {
		t.Fatalf("expected an already-informative market to have R2Market > 0.5, got %.4f", report.R2Market)
	}
	if report.DeltaR2 < -0.05 {
		t.Errorf("noisy fundamental should not meaningfully hurt the fit: DeltaR2=%.4f (R2Market=%.4f R2Combined=%.4f betaF=%.2f betaM=%.2f)",
			report.DeltaR2, report.R2Market, report.R2Combined, report.BetaFundamental, report.BetaMarket)
	}
}
