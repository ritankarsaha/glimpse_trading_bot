package strategy

import (
	"testing"

	"github.com/ritankarsaha/glimpse-bot/api"
)

// TestBestOddsExcludesLongshots is a regression test for the "implied prob
// >= 8%" floor: it used to compare YesPriceMillisats against 80 instead of
// 8000 (millisats = sats*1000, and 8% = 8 sats), so it let through outcomes
// down to ~0.08% implied probability instead of filtering out longshots.
func TestBestOddsExcludesLongshots(t *testing.T) {
	outcomes := []api.Outcome{
		{OptionID: 1, Name: "79800-80000", YesPriceMillisats: 500},   // 0.5%, a longshot
		{OptionID: 2, Name: "80000-80200", YesPriceMillisats: 40000}, // 40%, reasonable
		{OptionID: 3, Name: "80200-80400", YesPriceMillisats: 20000}, // 20%, reasonable and cheaper
	}

	s := &BestOdds{}
	got, err := s.SelectOption(outcomes, 80100, 50)
	if err != nil {
		t.Fatal(err)
	}
	if got.OptionID == 1 {
		t.Errorf("BestOdds picked the 0.5%%-probability longshot (option 1); the 8%% floor should have excluded it")
	}
	// Among the two outcomes that clear the 8% floor, it should pick the
	// cheaper one (option 3, 20 sats) over the pricier one (option 2, 40 sats).
	if got.OptionID != 3 {
		t.Errorf("BestOdds picked option %d, want option 3 (cheapest outcome clearing the 8%% floor)", got.OptionID)
	}
}

// TestBestOddsMinThresholdValue locks in the corrected constant so a future
// edit can't silently regress it back toward the old (broken) magnitude.
func TestBestOddsMinThresholdValue(t *testing.T) {
	if bestOddsMinYesPriceMillisats != 8000 {
		t.Errorf("bestOddsMinYesPriceMillisats = %d, want 8000 (8%% implied probability, at 1000 millisats/%%)", bestOddsMinYesPriceMillisats)
	}
}
