
package kelly

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

type Trade struct {
	BinIndex          int
	OptionID          int
	Range             string
	Contracts         int
	MarketProb        float64
	ModelProb         float64
	Edge              float64 
	YesPriceMillisats int64
	KellyContracts    float64
}

type Config struct {
	KellyFraction float64 
	Tau           float64 
	MinEdge       float64 
	MaxBins       int     
	MaxIterations int    
}

func DefaultConfig() Config {
	return Config{
		KellyFraction: 0.25,
		Tau:           lmsr.Tau,
		MinEdge:       0.005,
		MaxBins:       10,
		MaxIterations: 300,
	}
}

// OutcomeMap holds the full N-element quantity vector and a bin→outcome mapping
type OutcomeMap struct {
	Q            []float64          // q[i] = Shares + SubsidisedShares for bin i
	BinToOutcome map[int]*api.Outcome
}

// BuildOutcomeMap reconstructs the quantity vector from API market data
func BuildOutcomeMap(outcomes []api.Outcome) *OutcomeMap {
	q := make([]float64, lmsr.N)
	for i := range q {
		q[i] = lmsr.Q0
	}
	m := make(map[int]*api.Outcome, len(outcomes))
	for i := range outcomes {
		o := &outcomes[i]
		idx := parseBinIndex(o.Name)
		if idx < 0 || idx >= lmsr.N {
			continue
		}
		shares := float64(o.Shares + o.SubsidisedShares)
		if shares > 0 {
			q[idx] = shares
		}
		m[idx] = o
	}
	return &OutcomeMap{Q: q, BinToOutcome: m}
}

// parseBinIndex extracts the bin index from a range string like "80000-80200"
func parseBinIndex(name string) int {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return -1
	}
	lo, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return -1
	}
	idx := int(lo / lmsr.BinWidth)
	if idx < 0 || idx >= lmsr.N {
		return -1
	}
	return idx
}

// Optimize runs the full 4-stage Kelly pipeline
// budgetSats is the available capital in satoshis
func Optimize(om *OutcomeMap, modelProbs []float64, budgetSats float64, cfg Config) ([]Trade, error) {
	if len(modelProbs) != lmsr.N {
		return nil, fmt.Errorf("modelProbs must have %d elements, got %d", lmsr.N, len(modelProbs))
	}
	if budgetSats <= 0 {
		return nil, nil
	}

	q := om.Q

	// market-implied probs + underpriced set A
	mktPrices := lmsr.PriceVector(q)
	sumMktPrices := 0.0
	for _, p := range mktPrices {
		sumMktPrices += p
	}

	type candidate struct {
		idx  int
		mktP float64
		mdlP float64
		edge float64
	}
	var A []candidate
	for i := 0; i < lmsr.N; i++ {
		if _, ok := om.BinToOutcome[i]; !ok {
			continue
		}
		// Glimpse's LS-LMSR price components don't sum to 100 (they sum to
		// something that drifts with market state — 100·(V+1) at the
		// uniform seed, trending toward 100 as a market concentrates). The
		// platform's own normalization is price/Σprices, not price/100 —
		// dividing by a flat 100 overstates the market-implied probability
		// and makes real edges look smaller (or negative) than they are.
		mktP := mktPrices[i] / sumMktPrices
		mdlP := modelProbs[i]
		edge := mdlP - mktP
		if edge > cfg.MinEdge {
			A = append(A, candidate{i, mktP, mdlP, edge})
		}
	}
	if len(A) == 0 {
		return nil, nil
	}
	// Best opportunities first
	sort.Slice(A, func(i, j int) bool { return A[i].edge > A[j].edge })
	if len(A) > cfg.MaxBins {
		A = A[:cfg.MaxBins]
	}

	// truthful caps
	caps := make(map[int]float64, len(A))
	for _, c := range A {
		caps[c.idx] = lmsr.TruthfulCap(q, c.idx, c.mdlP)
	}

	//  Kelly optimisation via projected gradient ascent
	delta := make([]float64, lmsr.N)
	c0 := lmsr.CostFunction(q)
	step := budgetSats * 0.05

	var prevObj float64 = math.Inf(-1)
	stalls := 0

	for iter := 0; iter < cfg.MaxIterations; iter++ {
	
		qNew := make([]float64, lmsr.N)
		copy(qNew, q)
		for _, c := range A {
			qNew[c.idx] += delta[c.idx]
		}

		prices := lmsr.PriceVector(qNew)
		c1 := lmsr.CostFunction(qNew)
		totalCost := (1 + cfg.Tau) * (c1 - c0)

		// Settlement payout is 100 sats/contract with no fee — Glimpse's 2%
		// fee applies only to buying/selling, not to automatic settlement —
		// so the payout term is undiscounted (unlike totalCost above, which
		// correctly includes the entry fee).
		W := make([]float64, lmsr.N)
		for j := 0; j < lmsr.N; j++ {
			W[j] = budgetSats - totalCost + 100.0*delta[j]
			if W[j] < 1.0 {
				W[j] = 1.0
			}
		}

		// Objective
		obj := 0.0
		for j := 0; j < lmsr.N; j++ {
			obj += modelProbs[j] * math.Log(W[j])
		}

		if obj < prevObj-1e-9 {
			step *= 0.5
			if step < 1e-6 {
				break
			}
			stalls = 0
			continue
		}
		if math.Abs(obj-prevObj) < 1e-9 {
			stalls++
			if stalls > 5 {
				break
			}
		} else {
			stalls = 0
		}
		prevObj = obj

		// Σⱼ π*ⱼ/Wⱼ
		sumInvW := 0.0
		for j := 0; j < lmsr.N; j++ {
			sumInvW += modelProbs[j] / W[j]
		}

		// Gradient step + projection onto [0, cap]
		newDelta := make([]float64, lmsr.N)
		copy(newDelta, delta)
		for _, c := range A {
			i := c.idx
			grad := -(1+cfg.Tau)*prices[i]*sumInvW + modelProbs[i]*100.0/W[i]
			newDelta[i] = math.Max(0, math.Min(caps[i], newDelta[i]+step*grad))
		}

		// Enforce budget: scale down if needed
		qTest := make([]float64, lmsr.N)
		copy(qTest, q)
		for _, c := range A {
			qTest[c.idx] += newDelta[c.idx]
		}
		cTest := lmsr.CostFunction(qTest)
		if testCost := (1+cfg.Tau)*(cTest-c0); testCost > budgetSats {
			if testCost > 0 {
				scale := (budgetSats / testCost) * 0.95
				for _, c := range A {
					newDelta[c.idx] = math.Max(0, math.Min(caps[c.idx], delta[c.idx]*scale))
				}
			}
		}
		copy(delta, newDelta)
	}

	// fractional Kelly + floor
	var trades []Trade
	for _, c := range A {
		dKelly := delta[c.idx]
		dExec := math.Floor(math.Min(cfg.KellyFraction*dKelly, caps[c.idx]))
		if dExec < 1 {
			continue
		}
		o, ok := om.BinToOutcome[c.idx]
		if !ok {
			continue
		}
		trades = append(trades, Trade{
			BinIndex:          c.idx,
			OptionID:          o.OptionID,
			Range:             o.Name,
			Contracts:         int(dExec),
			MarketProb:        c.mktP,
			ModelProb:         c.mdlP,
			Edge:              c.edge,
			YesPriceMillisats: o.YesPriceMillisats,
			KellyContracts:    dKelly,
		})
	}
	return trades, nil
}

// ExpectedCost computes the exact LS-LMSR cost in sats to execute trades
// against market state q: (1+tau) * (C(q + Σdelta) - C(q)). The LMSR cost
// function is strictly convex, so buying multiple contracts on a bin moves
// that bin's price as you buy — cost is not linear in contract count, and a
// naive price×quantity sum understates the true (and actual exchange-
// charged) cost for anything beyond a single contract.
func ExpectedCost(q []float64, trades []Trade, tau float64) int64 {
	if len(trades) == 0 {
		return 0
	}
	qNew := make([]float64, len(q))
	copy(qNew, q)
	for _, t := range trades {
		if t.BinIndex >= 0 && t.BinIndex < len(qNew) {
			qNew[t.BinIndex] += float64(t.Contracts)
		}
	}
	c0 := lmsr.CostFunction(q)
	c1 := lmsr.CostFunction(qNew)
	return int64((1 + tau) * (c1 - c0))
}
