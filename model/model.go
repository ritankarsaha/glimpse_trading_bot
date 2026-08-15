
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ritankarsaha/glimpse-bot/lmsr"
)

// ForecastContext carries all signals available to a Forecaster.
// Models use what they need and ignore the rest, so adding new fields
// never breaks existing implementations.
type ForecastContext struct {
	SpotPrice      float64
	FearGreedIndex int
	TimeToExpiry   time.Duration // 0 means unknown/not provided
	ImpliedProbs   []float64     // live π̂ from the LMSR; nil means not provided
	RealizedVolAnn float64       // annualised vol, e.g. 0.65 for 65%; 0 means unknown
}

// Forecaster produces a probability distribution over lmsr.N price bins
type Forecaster interface {
	Name() string
	Forecast(ctx ForecastContext) ([]float64, error)
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

func (m *GaussianModel) Forecast(ctx ForecastContext) ([]float64, error) {
	sigma := gaussianSigma(ctx.SpotPrice, m.SigmaPct, ctx.RealizedVolAnn, ctx.TimeToExpiry)
	return gaussianBins(ctx.SpotPrice, sigma)
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

func (m *SkewedGaussianModel) Forecast(ctx ForecastContext) ([]float64, error) {
	// skewFactor: +0.5 when fng=0 (extreme fear -> bullish), -0.5 when fng=100
	skewFactor := float64(50-ctx.FearGreedIndex) / 100.0
	adjustedSpot := ctx.SpotPrice * (1.0 + skewFactor*m.SkewPct/100.0)
	sigma := gaussianSigma(ctx.SpotPrice, m.SigmaPct, ctx.RealizedVolAnn, ctx.TimeToExpiry)
	return gaussianBins(adjustedSpot, sigma)
}

// UniformModel assigns equal probability to every bin
type UniformModel struct{}

func (m *UniformModel) Name() string { return "uniform" }

func (m *UniformModel) Forecast(_ ForecastContext) ([]float64, error) {
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

func (m *JSONFileModel) Forecast(_ ForecastContext) ([]float64, error) {
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

// LogNormalModel assumes log(P_T) ~ N(μ, σ²T) where
// μ = log(spot) − σ²T/2 (the Itô drift correction) and
// σ = RealizedVolAnn from context (or DefaultAnnualVol as fallback).
// This gives the right-skewed distribution that matches BTC returns correctly.
type LogNormalModel struct {
	DefaultAnnualVol float64 // used when ctx.RealizedVolAnn == 0; e.g. 0.65
	DefaultTimeYears float64 // used when ctx.TimeToExpiry == 0; e.g. 6h/8760
}

func NewLogNormalModel(defaultAnnualVol, defaultTimeYears float64) *LogNormalModel {
	if defaultAnnualVol <= 0 {
		defaultAnnualVol = 0.65
	}
	if defaultTimeYears <= 0 {
		defaultTimeYears = 6.0 / 8760.0 // 6-hour market
	}
	return &LogNormalModel{
		DefaultAnnualVol: defaultAnnualVol,
		DefaultTimeYears: defaultTimeYears,
	}
}

func (m *LogNormalModel) Name() string {
	return fmt.Sprintf("lognormal(σ=%.0f%%)", m.DefaultAnnualVol*100)
}

func (m *LogNormalModel) Forecast(ctx ForecastContext) ([]float64, error) {
	if ctx.SpotPrice <= 0 {
		return nil, fmt.Errorf("lognormal: spot price must be positive, got %g", ctx.SpotPrice)
	}

	annualVol := ctx.RealizedVolAnn
	if annualVol <= 0 {
		annualVol = m.DefaultAnnualVol
	}

	var tYears float64
	if ctx.TimeToExpiry > 0 {
		tYears = ctx.TimeToExpiry.Hours() / 8760.0
	} else {
		tYears = m.DefaultTimeYears
	}

	// σ_T = annualVol × √T  (total vol over the horizon)
	sigmaT := annualVol * math.Sqrt(tYears)
	// Itô drift correction: μ = log(S₀) - ½σ²T
	mu := math.Log(ctx.SpotPrice) - 0.5*annualVol*annualVol*tYears

	return logNormalBins(mu, sigmaT)
}

// logNormalBins computes P(lo ≤ P_T < hi) for each bin using the log-normal CDF.
func logNormalBins(mu, sigma float64) ([]float64, error) {
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
			cdfHi = normCDF((math.Log(hi) - mu) / sigma)
		}

		var cdfLo float64
		if lo <= 0 {
			cdfLo = 0.0
		} else {
			cdfLo = normCDF((math.Log(lo) - mu) / sigma)
		}

		p := cdfHi - cdfLo
		if p < 0 {
			p = 0
		}
		probs[i] = p
	}
	return normalise(probs)
}

// HTTPForecaster delegates to an external HTTP endpoint.
// It POSTs ForecastContext as JSON and expects a JSON []float64 of length lmsr.N back.
// This lets any Python/LSTM/ARIMA model plug in without changing Go code.
//
// Note: ForecastContext.TimeToExpiry is serialised as nanoseconds (Go's time.Duration default).
type HTTPForecaster struct {
	URL     string
	Timeout time.Duration
}

func NewHTTPForecaster(url string, timeout time.Duration) *HTTPForecaster {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HTTPForecaster{URL: url, Timeout: timeout}
}

func (m *HTTPForecaster) Name() string { return fmt.Sprintf("http(%s)", m.URL) }

func (m *HTTPForecaster) Forecast(ctx ForecastContext) ([]float64, error) {
	body, err := json.Marshal(ctx)
	if err != nil {
		return nil, fmt.Errorf("http forecaster: marshaling context: %w", err)
	}

	client := &http.Client{Timeout: m.Timeout}
	resp, err := client.Post(m.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http forecaster: request to %s: %w", m.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http forecaster: %s returned status %d", m.URL, resp.StatusCode)
	}

	var probs []float64
	if err := json.NewDecoder(resp.Body).Decode(&probs); err != nil {
		return nil, fmt.Errorf("http forecaster: decoding response: %w", err)
	}
	if len(probs) != lmsr.N {
		return nil, fmt.Errorf("http forecaster: expected %d probabilities, got %d", lmsr.N, len(probs))
	}
	return normalise(probs)
}

// EnsembleForecaster blends multiple Forecasters via a weighted average.
// Weights must be non-negative and sum to 1.0.
// Example: 60% log-normal + 40% HTTP model.
type EnsembleForecaster struct {
	Models  []Forecaster
	Weights []float64
}

func NewEnsembleForecaster(models []Forecaster, weights []float64) (*EnsembleForecaster, error) {
	if len(models) == 0 {
		return nil, fmt.Errorf("ensemble: at least one model required")
	}
	if len(models) != len(weights) {
		return nil, fmt.Errorf("ensemble: %d models but %d weights", len(models), len(weights))
	}
	total := 0.0
	for i, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("ensemble: weight[%d] is negative (%g)", i, w)
		}
		total += w
	}
	if math.Abs(total-1.0) > 1e-9 {
		return nil, fmt.Errorf("ensemble: weights must sum to 1.0, got %.10f", total)
	}
	return &EnsembleForecaster{Models: models, Weights: weights}, nil
}

func (m *EnsembleForecaster) Name() string {
	parts := make([]string, len(m.Models))
	for i, mdl := range m.Models {
		parts[i] = fmt.Sprintf("%.2f×%s", m.Weights[i], mdl.Name())
	}
	return "ensemble(" + strings.Join(parts, " + ") + ")"
}

func (m *EnsembleForecaster) Forecast(ctx ForecastContext) ([]float64, error) {
	combined := make([]float64, lmsr.N)
	for j, mdl := range m.Models {
		probs, err := mdl.Forecast(ctx)
		if err != nil {
			return nil, fmt.Errorf("ensemble sub-model %q: %w", mdl.Name(), err)
		}
		for i, p := range probs {
			combined[i] += m.Weights[j] * p
		}
	}
	return normalise(combined)
}

// FromName constructs a Forecaster by name or JSON file path
func FromName(name string, sigmaPct float64) (Forecaster, error) {
	return FromNameWithCalibration(name, sigmaPct, "")
}

// CalibrationWeights holds the Benter two-stage logit weights fitted by
// backtest.FitLogitWeights and persisted to a JSON file.
type CalibrationWeights struct {
	BetaFundamental float64 `json:"beta_fundamental"`
	BetaMarket      float64 `json:"beta_market"`
}

// loadCalibrationWeights reads {betaF, betaM} from path. If path is empty or
// the file doesn't exist/parse, it returns the default geometric-mean prior
// (1.0, 1.0) — "shrink toward market" before any calibration data exists.
func loadCalibrationWeights(path string) CalibrationWeights {
	w := CalibrationWeights{BetaFundamental: 1.0, BetaMarket: 1.0}
	if path == "" {
		return w
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return w
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return CalibrationWeights{BetaFundamental: 1.0, BetaMarket: 1.0}
	}
	return w
}

// FromNameWithCalibration constructs a Forecaster by name or JSON file path.
// "benter" and "benter:<inner>" wrap <inner> (default "lognormal") in a
// LogitCombinerModel, loading calibrated {betaF, betaM} from calibrationPath
// if present (see CalibrationWeights).
func FromNameWithCalibration(name string, sigmaPct float64, calibrationPath string) (Forecaster, error) {
	if name == "benter" || strings.HasPrefix(name, "benter:") {
		inner := "lognormal"
		if idx := strings.Index(name, ":"); idx >= 0 && idx+1 < len(name) {
			inner = name[idx+1:]
		}
		fundamental, err := FromNameWithCalibration(inner, sigmaPct, calibrationPath)
		if err != nil {
			return nil, fmt.Errorf("benter: inner model %q: %w", inner, err)
		}
		weights := loadCalibrationWeights(calibrationPath)
		return NewLogitCombinerModel(fundamental, weights.BetaFundamental, weights.BetaMarket), nil
	}

	switch name {
	case "gaussian":
		return NewGaussianModel(sigmaPct), nil
	case "skewed":
		return NewSkewedGaussianModel(sigmaPct, 2.0), nil
	case "uniform":
		return &UniformModel{}, nil
	case "lognormal":
		return NewLogNormalModel(0, 0), nil
	default:
		if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") {
			return NewHTTPForecaster(name, 5*time.Second), nil
		}
		if _, err := os.Stat(name); err == nil {
			return &JSONFileModel{Path: name}, nil
		}
		return nil, fmt.Errorf("unknown model %q: choose gaussian, skewed, uniform, lognormal, benter[:inner], an HTTP(S) URL, or a JSON file path", name)
	}
}

// gaussianSigma returns the Gaussian σ to use.
// When annualVol > 0 and T > 0 the correct formula is used: σ = spot × annualVol × √(T/1yr).
// Otherwise falls back to the fixed sigmaPct of spot.
func gaussianSigma(spot, sigmaPct, annualVol float64, T time.Duration) float64 {
	if annualVol > 0 && T > 0 {
		tYears := T.Hours() / 8760.0
		return spot * annualVol * math.Sqrt(tYears)
	}
	return spot * sigmaPct / 100.0
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
