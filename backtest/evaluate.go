package backtest

import (
	"fmt"
	"math"
	"sort"

	"github.com/ritankarsaha/glimpse-bot/model"
)

const llEpsilon = 1e-12

const minSamplesForFit = 30

// LogLikelihood returns Σ log(probs[i][outcomes[i]]).
func LogLikelihood(probs [][]float64, outcomes []int) float64 {
	ll := 0.0
	for i, y := range outcomes {
		p := probs[i][y]
		if p < llEpsilon {
			p = llEpsilon
		}
		ll += math.Log(p)
	}
	return ll
}

// NullLogLikelihood is the log-likelihood of the uniform baseline over n
// bins for nSamples observations: nSamples * log(1/n).
func NullLogLikelihood(n, nSamples int) float64 {
	return float64(nSamples) * math.Log(1.0/float64(n))
}

// PseudoR2 is McFadden's pseudo-R²: 1 - LL(model)/LL(null). Both LLs are
// negative, so a model fitting better than uniform gives R² in (0,1]; a
// model fitting worse gives R² < 0.
func PseudoR2(modelLL, nullLL float64) float64 {
	if nullLL == 0 {
		return 0
	}
	return 1 - modelLL/nullLL
}

// Report summarizes a calibration run on the holdout split: how much the
// fundamental model adds over the market alone (DeltaR2 = R2Combined -
// R2Market), and the Benter weights that produced it.
type Report struct {
	NSamples        int
	NTrain          int
	NHoldout        int
	R2Market        float64
	R2Combined      float64
	DeltaR2         float64
	BetaFundamental float64
	BetaMarket      float64
}

func normalizeProbs(p []float64) []float64 {
	sum := 0.0
	for _, v := range p {
		sum += v
	}
	out := make([]float64, len(p))
	for i, v := range p {
		out[i] = v / sum
	}
	return out
}

// FitLogitWeights splits samples chronologically: the earliest 1-holdoutFrac
// form the training set, the most recent holdoutFrac form the holdout set
// ("out of sample" = future data, per Benter). It grid-searches
// betaF, betaM in [0,3] (step 0.25) to maximize training log-likelihood via
// model.CombineLogOdds, then reports R2(market) vs R2(combined) on the
// holdout set using the fitted weights. Only resolved samples (OutcomeBin >=
// 0) are used. Returns an error if fewer than minSamplesForFit are resolved.
func FitLogitWeights(samples []Sample, holdoutFrac float64) (Report, error) {
	resolved := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if s.OutcomeBin >= 0 {
			resolved = append(resolved, s)
		}
	}
	if len(resolved) < minSamplesForFit {
		return Report{}, fmt.Errorf("need at least %d resolved samples to fit, got %d", minSamplesForFit, len(resolved))
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Timestamp.Before(resolved[j].Timestamp)
	})

	if holdoutFrac <= 0 || holdoutFrac >= 1 {
		holdoutFrac = 0.2
	}
	nHoldout := int(float64(len(resolved)) * holdoutFrac)
	if nHoldout < 1 {
		nHoldout = 1
	}
	if nHoldout >= len(resolved) {
		nHoldout = len(resolved) - 1
	}
	train := resolved[:len(resolved)-nHoldout]
	holdout := resolved[len(resolved)-nHoldout:]

	bestBetaF, bestBetaM := 1.0, 1.0
	bestLL := math.Inf(-1)
	trainOutcomes := make([]int, len(train))
	for i, s := range train {
		trainOutcomes[i] = s.OutcomeBin
	}
	for bf := 0.0; bf <= 3.0; bf += 0.25 {
		for bm := 0.0; bm <= 3.0; bm += 0.25 {
			probs := make([][]float64, len(train))
			for i, s := range train {
				probs[i] = model.CombineLogOdds(s.FundamentalProbs, s.MarketProbs, bf, bm)
			}
			ll := LogLikelihood(probs, trainOutcomes)
			if ll > bestLL {
				bestLL = ll
				bestBetaF, bestBetaM = bf, bm
			}
		}
	}

	n := len(holdout[0].FundamentalProbs)
	marketProbs := make([][]float64, len(holdout))
	combinedProbs := make([][]float64, len(holdout))
	holdoutOutcomes := make([]int, len(holdout))
	for i, s := range holdout {
		marketProbs[i] = normalizeProbs(s.MarketProbs)
		combinedProbs[i] = model.CombineLogOdds(s.FundamentalProbs, s.MarketProbs, bestBetaF, bestBetaM)
		holdoutOutcomes[i] = s.OutcomeBin
	}

	nullLL := NullLogLikelihood(n, len(holdout))
	marketLL := LogLikelihood(marketProbs, holdoutOutcomes)
	combinedLL := LogLikelihood(combinedProbs, holdoutOutcomes)

	r2Market := PseudoR2(marketLL, nullLL)
	r2Combined := PseudoR2(combinedLL, nullLL)

	return Report{
		NSamples:        len(resolved),
		NTrain:          len(train),
		NHoldout:        len(holdout),
		R2Market:        r2Market,
		R2Combined:      r2Combined,
		DeltaR2:         r2Combined - r2Market,
		BetaFundamental: bestBetaF,
		BetaMarket:      bestBetaM,
	}, nil
}
