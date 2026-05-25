
package model

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// Forecaster produces a probability distribution over lmsr.N price bins
type Forecaster interface {
	Name() string
	Forecast(spotPrice float64, fng int) ([]float64, error)
}

// GaussianModel places a Gaussian (normal) distribution over bins,
// centred on spotPrice with standard deviation = SigmaPct% of spot
type GaussianModel struct {
	SigmaPct float64 // σ as % of spot; e.g. 5.0 → σ = 5% of $82k ≈ $4100
}

func NewGaussianModel(sigmaPct float64) *GaussianModel {
	if sigmaPct <= 0 {
		sigmaPct = 5.0
	}
	return &GaussianModel{SigmaPct: sigmaPct}
}

func (m *GaussianModel) Name() string {
	return fmt.Sprintf("gaussian(σ=%.1f%%)", m.SigmaPct)
}

func (m *GaussianModel) Forecast(spot float64, _ int) ([]float64, error) {
	sigma := spot * m.SigmaPct / 100.0
	return gaussianBins(spot, sigma)
}

// SkewedGaussianModel is a Gaussian model that shifts its mean according to
// the Fear & Greed index (contrarian signal: fear -> expect recovery, greed -> expect correction)
type SkewedGaussianModel struct {
	SigmaPct float64
	SkewPct  float64 // max mean-shift as % of spot; default 2.0
}

func NewSkewedGaussianModel(sigmaPct, skewPct float64) *SkewedGaussianModel {
	if sigmaPct <= 0 {
		sigmaPct = 5.0
	}
	if skewPct <= 0 {
		skewPct = 2.0
	}
	return &SkewedGaussianModel{SigmaPct: sigmaPct, SkewPct: skewPct}
}

func (m *SkewedGaussianModel) Name() string {
	return fmt.Sprintf("skewed-gaussian(σ=%.1f%%,skew=%.1f%%)", m.SigmaPct, m.SkewPct)
}

func (m *SkewedGaussianModel) Forecast(spot float64, fng int) ([]float64, error) {
	// skewFactor: +0.5 when fng=0 (extreme fear -> bullish), -0.5 when fng=100
	skewFactor := float64(50-fng) / 100.0
	adjustedSpot := spot * (1.0 + skewFactor*m.SkewPct/100.0)
	sigma := spot * m.SigmaPct / 100.0
	return gaussianBins(adjustedSpot, sigma)
}

// UniformModel assigns equal probability to every bin
type UniformModel struct{}

func (m *UniformModel) Name() string { return "uniform" }

func (m *UniformModel) Forecast(_ float64, _ int) ([]float64, error) {
	probs := make([]float64, lmsr.N)
	for i := range probs {
		probs[i] = 1.0 / lmsr.N
	}
	return probs, nil
}

// JSONFileModel loads a probability vector from a JSON file
type JSONFileModel struct {
	Path string
}

func (m *JSONFileModel) Name() string { return fmt.Sprintf("json(%s)", m.Path) }

func (m *JSONFileModel) Forecast(_ float64, _ int) ([]float64, error) {
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return nil, fmt.Errorf("reading model file: %w", err)
	}
	var probs []float64
	if err := json.Unmarshal(data, &probs); err != nil {
		return nil, fmt.Errorf("parsing model file: %w", err)
	}
	if len(probs) != lmsr.N {
		return nil, fmt.Errorf("expected %d probabilities, got %d", lmsr.N, len(probs))
	}
	return normalise(probs)
}

// FromName constructs a Forecaster by name or JSON file path
func FromName(name string, sigmaPct float64) (Forecaster, error) {
	switch name {
	case "gaussian":
		return NewGaussianModel(sigmaPct), nil
	case "skewed":
		return NewSkewedGaussianModel(sigmaPct, 2.0), nil
	case "uniform":
		return &UniformModel{}, nil
	default:
		if _, err := os.Stat(name); err == nil {
			return &JSONFileModel{Path: name}, nil
		}
		return nil, fmt.Errorf("unknown model %q: choose gaussian, skewed, uniform, or a JSON file path", name)
	}
}

// gaussianBins computes P(price in bin i) = Φ((hi−μ)/σ) − Φ((lo−μ)/σ) for each bin
func gaussianBins(mu, sigma float64) ([]float64, error) {
	if sigma <= 0 {
		return nil, fmt.Errorf("sigma must be positive")
	}
	probs := make([]float64, lmsr.N)
	for i := 0; i < lmsr.N; i++ {
		lo := lmsr.BinLow(i)
		hi := lmsr.BinHigh(i)
		var cdfHi float64
		if math.IsInf(hi, 1) {
			cdfHi = 1.0
		} else {
			cdfHi = normCDF((hi - mu) / sigma)
		}
		cdfLo := normCDF((lo - mu) / sigma)
		p := cdfHi - cdfLo
		if p < 0 {
			p = 0
		}
		probs[i] = p
	}
	return normalise(probs)
}

func normalise(probs []float64) ([]float64, error) {
	total := 0.0
	for _, p := range probs {
		total += p
	}
	if total <= 0 {
		return nil, fmt.Errorf("probability vector sums to zero")
	}
	out := make([]float64, len(probs))
	for i, p := range probs {
		out[i] = p / total
	}
	return out, nil
}

func normCDF(x float64) float64 {
	return 0.5 * (1.0 + math.Erf(x/math.Sqrt2))
}
