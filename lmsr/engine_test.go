package lmsr

import (
	"math"
	"testing"
)


func uniformQ() []float64 {
	q := make([]float64, N)
	for i := range q {
		q[i] = Q0
	}
	return q
}

func TestCostFunctionUniform(t *testing.T) {
	want := 100.0 * Q0 * (V + 1) // 150,000
	got := CostFunction(uniformQ())
	if math.Abs(got-want)/want > 1e-9 {
		t.Errorf("CostFunction(q₀) = %.6f, want %.6f", got, want)
	}
}

func TestPriceVectorUniform(t *testing.T) {
	want := 100.0 * (V + 1) / N // 0.6
	prices := PriceVector(uniformQ())
	for i, p := range prices {
		if math.Abs(p-want)/want > 1e-9 {
			t.Errorf("PriceVector[%d] = %.8f, want %.8f", i, p, want)
			break
		}
	}
	// Sum of prices = 100·(V+1) = 300
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	wantSum := 100.0 * (V + 1)
	if math.Abs(sum-wantSum)/wantSum > 1e-9 {
		t.Errorf("Σpᵢ = %.6f, want %.6f", sum, wantSum)
	}
}

func TestImpliedProbsUniform(t *testing.T) {
	probs := ImpliedProbs(uniformQ())
	wantEach := (V + 1) / N // 0.006
	for i, p := range probs {
		if math.Abs(p-wantEach) > 1e-10 {
			t.Errorf("ImpliedProbs[%d] = %v, want %v", i, p, wantEach)
			break
		}
	}
}

func TestPriceAtUpdateMonotone(t *testing.T) {
	q := uniformQ()
	// pᵢ should increase as delta increases
	p0 := PriceAtUpdate(q, 250, 0)
	p1 := PriceAtUpdate(q, 250, 1000)
	p2 := PriceAtUpdate(q, 250, 100000)
	if !(p0 < p1 && p1 < p2) {
		t.Errorf("price not monotone: p(0)=%v p(1000)=%v p(100000)=%v", p0, p1, p2)
	}
}

func TestTruthfulCapRoundTrip(t *testing.T) {
	q := uniformQ()
	targetProb := 0.05 // 5%, well above the market-implied 0.006 at uniform state
	cap := TruthfulCap(q, 250, targetProb)
	if cap <= 0 {
		t.Fatalf("TruthfulCap returned %.4f; expected positive value", cap)
	}
	// Verify price at cap ≈ 100·targetProb
	gotPrice := PriceAtUpdate(q, 250, cap)
	wantPrice := 100.0 * targetProb
	// Tolerance binary search stops at 0.5-contract precision -> ≤ 0.2% price error.
	if math.Abs(gotPrice-wantPrice)/wantPrice > 0.003 {
		t.Errorf("price after cap = %.6f, want %.6f (err %.2e)", gotPrice, wantPrice, math.Abs(gotPrice-wantPrice)/wantPrice)
	}
}

func TestBinHelpers(t *testing.T) {
	if BinLow(400) != 80000 {
		t.Errorf("BinLow(400) = %v, want 80000", BinLow(400))
	}
	if BinHigh(400) != 80200 {
		t.Errorf("BinHigh(400) = %v, want 80200", BinHigh(400))
	}
	if SpotToBin(80100) != 400 {
		t.Errorf("SpotToBin(80100) = %d, want 400", SpotToBin(80100))
	}
	if SpotToBin(80200) != 401 {
		t.Errorf("SpotToBin(80200) = %d, want 401", SpotToBin(80200))
	}
}
