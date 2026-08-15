package model

import (
	"fmt"
	"math"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// logitEpsilon floors probabilities before taking logs so that a zero
// probability in either input doesn't produce -Inf scores.
const logitEpsilon = 1e-12

// CombineLogOdds blends two probability vectors via Benter's (1994) two-stage
// logit combination: score_i = betaF*log(pf_i+eps) + betaM*log(pm_i+eps),
// softmax-normalized. pm need not sum to 1 — Glimpse's lmsr.ImpliedProbs sums
// to (lmsr.V+1); any constant scale factor on pm cancels in the softmax.
func CombineLogOdds(pf, pm []float64, betaF, betaM float64) []float64 {
	n := len(pf)
	scores := make([]float64, n)
	maxScore := math.Inf(-1)
	for i := 0; i < n; i++ {
		scores[i] = betaF*math.Log(pf[i]+logitEpsilon) + betaM*math.Log(pm[i]+logitEpsilon)
		if scores[i] > maxScore {
			maxScore = scores[i]
		}
	}
	out := make([]float64, n)
	sum := 0.0
	for i, s := range scores {
		out[i] = math.Exp(s - maxScore)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// LogitCombinerModel implements Benter's (1994) two-stage combination of a
// "fundamental" forecast with the market's own implied probabilities,
// blended in log-odds space. Default weights βF=βM=1 (geometric mean) shrink
// the fundamental view toward the market until calibration data is available.
type LogitCombinerModel struct {
	Fundamental     Forecaster
	BetaFundamental float64
	BetaMarket      float64
}

func NewLogitCombinerModel(fundamental Forecaster, betaF, betaM float64) *LogitCombinerModel {
	return &LogitCombinerModel{Fundamental: fundamental, BetaFundamental: betaF, BetaMarket: betaM}
}

func (m *LogitCombinerModel) Name() string {
	return fmt.Sprintf("benter(βf=%.2f,βm=%.2f)+%s", m.BetaFundamental, m.BetaMarket, m.Fundamental.Name())
}

// Forecast blends the fundamental model's probabilities with ctx.ImpliedProbs.
// If ImpliedProbs is unavailable (nil or wrong length), the fundamental
// forecast is returned unchanged — there's nothing to combine with.
func (m *LogitCombinerModel) Forecast(ctx ForecastContext) ([]float64, error) {
	pf, err := m.Fundamental.Forecast(ctx)
	if err != nil {
		return nil, fmt.Errorf("benter combiner: fundamental %q: %w", m.Fundamental.Name(), err)
	}
	if len(ctx.ImpliedProbs) != lmsr.N {
		return pf, nil
	}
	return CombineLogOdds(pf, ctx.ImpliedProbs, m.BetaFundamental, m.BetaMarket), nil
}

// FundamentalProbs returns the "fundamental" signal for f: for a
// LogitCombinerModel that's the wrapped model's raw output (pre-blend); for
// any other Forecaster it's just f.Forecast(ctx). Used by the sample recorder
// so calibration always sees the fundamental view, even when the bot is
// actively trading on a combined (benter) model.
func FundamentalProbs(f Forecaster, ctx ForecastContext) ([]float64, error) {
	if c, ok := f.(*LogitCombinerModel); ok {
		return c.Fundamental.Forecast(ctx)
	}
	return f.Forecast(ctx)
}
