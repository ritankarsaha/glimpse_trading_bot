package strategy

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ritankarsaha/glimpse-bot/api"
)

// Strategy selects which outcome to trade given market data
type Strategy interface {
	Name() string
	SelectOption(outcomes []api.Outcome, spotPrice float64, fng int) (*api.Outcome, error)
}

func parseRange(name string) (low, high float64, err error) {
	parts := strings.SplitN(name, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected range format: %q", name)
	}
	low, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing low bound of %q: %w", name, err)
	}
	high, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parsing high bound of %q: %w", name, err)
	}
	return low, high, nil
}

type RankedOutcome struct {
	Outcome *api.Outcome
	Dist    float64
}

func RankByProximity(outcomes []api.Outcome, target float64) []RankedOutcome {
	var ranked []RankedOutcome
	for i := range outcomes {
		low, high, err := parseRange(outcomes[i].Name)
		if err != nil {
			continue
		}
		mid := (low + high) / 2
		ranked = append(ranked, RankedOutcome{
			Outcome: &outcomes[i],
			Dist:    math.Abs(mid - target),
		})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].Dist < ranked[j].Dist
	})
	return ranked
}

func findNearest(outcomes []api.Outcome, target float64) (*api.Outcome, error) {
	for i := range outcomes {
		low, high, err := parseRange(outcomes[i].Name)
		if err != nil {
			continue
		}
		if target >= low && target < high {
			return &outcomes[i], nil
		}
	}
	ranked := RankByProximity(outcomes, target)
	if len(ranked) == 0 {
		return nil, fmt.Errorf("no parseable outcomes available")
	}
	return ranked[0].Outcome, nil
}

// NearestToSpot: picks the range containing spot, or the closest midpoint.
type NearestToSpot struct{}

func (s *NearestToSpot) Name() string { return "nearest" }

func (s *NearestToSpot) SelectOption(outcomes []api.Outcome, spotPrice float64, fng int) (*api.Outcome, error) {
	return findNearest(outcomes, spotPrice)
}

// FearAndGreedStrategy: contrarian — when market is fearful, bet bullish; when greedy, bet bearish
// Offset scales with conviction: extreme readings (< 20 or > 80) use 1.5%, moderate use 0.75%
type FearAndGreedStrategy struct{}

func (s *FearAndGreedStrategy) Name() string { return "feargreed" }

func (s *FearAndGreedStrategy) SelectOption(outcomes []api.Outcome, spotPrice float64, fng int) (*api.Outcome, error) {
	adjusted := spotPrice
	switch {
	case fng < 20:
		adjusted = spotPrice * 1.015 
	case fng < 30:
		adjusted = spotPrice * 1.0075 
	case fng > 80:
		adjusted = spotPrice * 0.985 
	case fng > 70:
		adjusted = spotPrice * 0.9925 
	}
	return findNearest(outcomes, adjusted)
}

type BestOdds struct{}

func (s *BestOdds) Name() string { return "bestodds" }

func (s *BestOdds) SelectOption(outcomes []api.Outcome, spotPrice float64, fng int) (*api.Outcome, error) {
	ranked := RankByProximity(outcomes, spotPrice)
	if len(ranked) == 0 {
		return nil, fmt.Errorf("no parseable outcomes available")
	}

	// Look at up to 3 nearest ranges.
	window := ranked
	if len(window) > 3 {
		window = window[:3]
	}

	// Among those, pick lowest yes_price (best return) with implied prob >= 8%
	var best *api.Outcome
	for _, r := range window {
		if r.Outcome.YesPriceMillisats < 80 {
			continue 
		}
		if best == nil || r.Outcome.YesPriceMillisats < best.YesPriceMillisats {
			best = r.Outcome
		}
	}
	if best != nil {
		return best, nil
	}
	// Fallback: just return nearest
	return ranked[0].Outcome, nil
}

type FollowCrowd struct{}

func (s *FollowCrowd) Name() string { return "followcrowd" }

func (s *FollowCrowd) SelectOption(outcomes []api.Outcome, spotPrice float64, fng int) (*api.Outcome, error) {
	if len(outcomes) == 0 {
		return nil, fmt.Errorf("no outcomes available")
	}
	best := &outcomes[0]
	for i := 1; i < len(outcomes); i++ {
		if outcomes[i].YesPriceMillisats > best.YesPriceMillisats {
			best = &outcomes[i]
		}
	}
	return best, nil
}

func FromName(name string) (Strategy, error) {
	switch name {
	case "nearest":
		return &NearestToSpot{}, nil
	case "feargreed":
		return &FearAndGreedStrategy{}, nil
	case "bestodds":
		return &BestOdds{}, nil
	case "followcrowd":
		return &FollowCrowd{}, nil
	default:
		return nil, fmt.Errorf("unknown strategy %q: choose nearest, feargreed, bestodds, or followcrowd", name)
	}
}
