package kelly

import "math"

// KellyFraction returns Benter's f* = p - (1-p)*c/(1-c), the full-Kelly
// fraction of bankroll to stake on an outcome priced at c (=market-implied
// probability, price/100) when the trader believes the true probability is p.
// Returns 0 when c is outside (0,1) or there is no edge (f* <= 0).
func KellyFraction(p, c float64) float64 {
	if c <= 0 || c >= 1 {
		return 0
	}
	f := p - (1-p)*c/(1-c)
	if f < 0 {
		return 0
	}
	return f
}

// ExpectedLogGrowth returns the expected per-bet log-growth of bankroll when
// staking fraction f on an outcome priced at c, given the *true* probability
// pTrue of that outcome occurring:
//
//	E[log growth] = pTrue*log(1-f+f/c) + (1-pTrue)*log(1-f)
//
// f is clamped to [0, 0.999] (betting the entire bankroll makes log(1-f)
// diverge to -Inf).
func ExpectedLogGrowth(c, pTrue, f float64) float64 {
	if f < 0 {
		f = 0
	}
	if f > 0.999 {
		f = 0.999
	}
	return pTrue*math.Log(1-f+f/c) + (1-pTrue)*math.Log(1-f)
}

// OverestimationImpact demonstrates Benter's warning (§4.2.5): if a trader's
// perceived edge is overestimated by overestimateFactor (true edge =
// perceived edge / overestimateFactor), sizing the bet for the *perceived*
// edge can shrink — rather than grow — the bankroll.
//
// f = kellyFraction * KellyFraction(pPerceived, c) is the executed stake.
//   - growthIfCorrect is the expected log-growth if pPerceived were correct.
//   - growthTrue is the actual expected log-growth given the true
//     probability pTrue = c + (pPerceived-c)/overestimateFactor.
func OverestimationImpact(c, pPerceived, kellyFraction, overestimateFactor float64) (growthIfCorrect, growthTrue float64) {
	f := kellyFraction * KellyFraction(pPerceived, c)
	growthIfCorrect = ExpectedLogGrowth(c, pPerceived, f)

	pTrue := c + (pPerceived-c)/overestimateFactor
	growthTrue = ExpectedLogGrowth(c, pTrue, f)
	return growthIfCorrect, growthTrue
}
