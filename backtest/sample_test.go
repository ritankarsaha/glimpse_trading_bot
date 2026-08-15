package backtest

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "forecasts.jsonl")
	r := NewRecorder(path)

	s1 := Sample{
		Timestamp:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TopicID:          1116,
		FundamentalProbs: []float64{0.1, 0.9},
		MarketProbs:      []float64{0.3, 0.3},
		OutcomeBin:       -1,
	}
	s2 := s1
	s2.Timestamp = s1.Timestamp.Add(time.Hour)
	s2.OutcomeBin = 1

	if err := r.Record(s1); err != nil {
		t.Fatal(err)
	}
	if err := r.Record(s2); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d samples, want 2", len(got))
	}
	if got[0].OutcomeBin != -1 || got[1].OutcomeBin != 1 {
		t.Errorf("unexpected outcome bins: %d, %d", got[0].OutcomeBin, got[1].OutcomeBin)
	}
	if got[1].TopicID != 1116 {
		t.Errorf("topic id = %d, want 1116", got[1].TopicID)
	}
}

func TestRecorderNilAndEmptyPathAreNoOps(t *testing.T) {
	var r *Recorder
	if err := r.Record(Sample{}); err != nil {
		t.Errorf("nil recorder should be a no-op, got %v", err)
	}

	r2 := NewRecorder("")
	if err := r2.Record(Sample{}); err != nil {
		t.Errorf("empty-path recorder should be a no-op, got %v", err)
	}
}

func TestLoadSamplesMissingFile(t *testing.T) {
	_, err := LoadSamples(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}
