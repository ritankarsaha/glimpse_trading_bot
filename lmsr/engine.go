package lmsr

import "math"

const (
	N        = 500  
	V        = 2.0   
	Tau      = 0.02  
	Q0       = 500.0 
	BinWidth = 200.0 
)

// Alpha returns α = v/(n·ln n), the LS-LMSR liquidity parameter.
func Alpha() float64 { return V / (N * math.Log(N)) }

// BParam computes b(q) = α · Σqⱼ
func BParam(q []float64) float64 {
	s := 0.0
	for _, v := range q {
		s += v
	}
	return Alpha() * s
}

// lse computes log(Σ exp(x[i])) stably and returns both the scalar
func lse(x []float64) (float64, []float64) {
	mx := x[0]
	for _, v := range x[1:] {
		if v > mx {
			mx = v
		}
	}
	es := make([]float64, len(x))
	s := 0.0
	for i, v := range x {
		es[i] = math.Exp(v - mx)
		s += es[i]
	}
	return mx + math.Log(s), es
}

// CostFunction computes C(q) = 100·b·ln(Σ exp(qᵢ/b))
func CostFunction(q []float64) float64 {
	b := BParam(q)
	if b <= 0 {
		return 0
	}
	sc := make([]float64, len(q))
	for i, qi := range q {
		sc[i] = qi / b
	}
	l, _ := lse(sc)
	return 100.0 * b * l
}

// PriceVector computes pᵢ(q) = ∂C/∂qᵢ for all i

func PriceVector(q []float64) []float64 {
	n := len(q)
	b := BParam(q)
	alpha := Alpha()

	sc := make([]float64, n)
	for i, qi := range q {
		sc[i] = qi / b
	}

	logS, es := lse(sc)
	sumES := 0.0
	for _, e := range es {
		sumES += e
	}

	// Q̃/(b·S) = Σ qⱼ·es[j] / (b·sumES)
	qw := 0.0
	for i, qi := range q {
		qw += qi * es[i]
	}
	qtOverBS := qw / (b * sumES)

	out := make([]float64, n)
	common := 100.0 * (alpha*logS - alpha*qtOverBS)
	for i := range q {
		out[i] = common + 100.0*es[i]/sumES
	}
	return out
}

// PriceAtUpdate computes pᵢ(q + delta·eᵢ) — price of bin idx after buying
func PriceAtUpdate(q []float64, idx int, delta float64) float64 {
	b := BParam(q)
	alpha := Alpha()
	bNew := b + alpha*delta

	n := len(q)
	sc := make([]float64, n)
	for i, qi := range q {
		sc[i] = qi / bNew
	}
	sc[idx] = (q[idx] + delta) / bNew

	logS, es := lse(sc)
	sumES := 0.0
	for _, e := range es {
		sumES += e
	}

	qw := 0.0
	for i, qi := range q {
		if i == idx {
			qw += (qi + delta) * es[i]
		} else {
			qw += qi * es[i]
		}
	}

	return 100.0 * (alpha*logS + es[idx]/sumES - alpha*qw/(bNew*sumES))
}

// ImpliedProbs returns π̂ᵢ = pᵢ(q)/100
func ImpliedProbs(q []float64) []float64 {
	prices := PriceVector(q)
	out := make([]float64, len(prices))
	for i, p := range prices {
		out[i] = p / 100.0
	}
	return out
}

// TruthfulCap finds Δᵢᵐᵃˣ such that pᵢ(q + Δ·eᵢ) = 100·targetProb via
// binary search (pᵢ is strictly increasing in Δ since C is strictly convex)
func TruthfulCap(q []float64, idx int, targetProb float64) float64 {
	target := 100.0 * targetProb
	if PriceAtUpdate(q, idx, 0) >= target {
		return 0
	}
	hi := 200.0
	for PriceAtUpdate(q, idx, hi) < target && hi < 1e10 {
		hi *= 4
	}
	lo := 0.0
	for i := 0; i < 64; i++ {
		mid := (lo + hi) / 2
		if PriceAtUpdate(q, idx, mid) < target {
			lo = mid
		} else {
			hi = mid
		}
		if hi-lo < 0.5 {
			break
		}
	}
	return lo
}

// BinLow returns the lower USD bound of bin i
func BinLow(i int) float64 { return float64(i) * BinWidth }

func BinHigh(i int) float64 {
	if i >= N-1 {
		return math.Inf(1)
	}
	return float64(i+1) * BinWidth
}

// BinMid returns a representative mid-price for display
func BinMid(i int) float64 {
	if i >= N-1 {
		return BinLow(i) + BinWidth
	}
	return BinLow(i) + BinWidth/2
}

// SpotToBin returns the bin index that contains price
func SpotToBin(price float64) int {
	idx := int(price / BinWidth)
	if idx < 0 {
		return 0
	}
	if idx >= N {
		return N - 1
	}
	return idx
}
