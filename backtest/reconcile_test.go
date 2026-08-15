package backtest

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReconcileSetsOutcomeBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forecasts.jsonl")
	r := NewRecorder(path)

	expiry := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	samples := []Sample{
		{Timestamp: expiry.Add(-2 * time.Hour), TopicID: 1116, FundamentalProbs: []float64{0.5, 0.5}, MarketProbs: []float64{0.5, 0.5}, OutcomeBin: -1},
		{Timestamp: expiry.Add(-1 * time.Hour), TopicID: 1116, FundamentalProbs: []float64{0.4, 0.6}, MarketProbs: []float64{0.5, 0.5}, OutcomeBin: -1},
		{Timestamp: expiry.Add(-1 * time.Hour), TopicID: 9999, FundamentalProbs: []float64{0.4, 0.6}, MarketProbs: []float64{0.5, 0.5}, OutcomeBin: -1},
		{Timestamp: expiry.Add(-1 * time.Hour), TopicID: 1116, FundamentalProbs: []float64{0.4, 0.6}, MarketProbs: []float64{0.5, 0.5}, OutcomeBin: 1},
	}
	for _, s := range samples {
		if err := r.Record(s); err != nil {
			t.Fatal(err)
		}
	}

	if err := Reconcile(path, 1116, expiry, 0); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d samples, want 4", len(got))
	}
	if got[0].OutcomeBin != 0 {
		t.Errorf("sample 0 (topic 1116, unresolved, before expiry) outcome=%d, want 0", got[0].OutcomeBin)
	}
	if got[1].OutcomeBin != 0 {
		t.Errorf("sample 1 (topic 1116, unresolved, before expiry) outcome=%d, want 0", got[1].OutcomeBin)
	}
	if got[2].OutcomeBin != -1 {
		t.Errorf("sample 2 (different topic) outcome=%d, want -1 (untouched)", got[2].OutcomeBin)
	}
	if got[3].OutcomeBin != 1 {
		t.Errorf("sample 3 (already resolved) outcome=%d, want 1 (untouched)", got[3].OutcomeBin)
	}
}

func TestReconcileMissingFileIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.jsonl")
	if err := Reconcile(path, 1116, time.Now(), 0); err != nil {
		t.Errorf("reconciling a missing file should be a no-op, got %v", err)
	}
}

func TestReconcileNoMatchesIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forecasts.jsonl")
	r := NewRecorder(path)
	s := Sample{Timestamp: time.Now(), TopicID: 1116, FundamentalProbs: []float64{0.5, 0.5}, MarketProbs: []float64{0.5, 0.5}, OutcomeBin: -1}
	if err := r.Record(s); err != nil {
		t.Fatal(err)
	}

	// cutoff before the sample's timestamp -> no match
	if err := Reconcile(path, 1116, time.Now().Add(-time.Hour), 0); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].OutcomeBin != -1 {
		t.Errorf("outcome=%d, want -1 (no match expected)", got[0].OutcomeBin)
	}
}
