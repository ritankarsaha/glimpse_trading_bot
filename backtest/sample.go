package backtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Sample struct {
	Timestamp        time.Time `json:"timestamp"`
	TopicID          int       `json:"topic_id"`
	FundamentalProbs []float64 `json:"fundamental_probs"`
	MarketProbs      []float64 `json:"market_probs"`
	OutcomeBin       int       `json:"outcome_bin"`
}

type Recorder struct {
	path string
	mu   sync.Mutex
}

func NewRecorder(path string) *Recorder {
	return &Recorder{path: path}
}

func (r *Recorder) Record(s Sample) error {
	if r == nil || r.path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if dir := filepath.Dir(r.path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating samples dir: %w", err)
		}
	}

	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening samples file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshalling sample: %w", err)
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing sample: %w", err)
	}
	return nil
}

// LoadSamples reads all Samples from a JSONL file.
func LoadSamples(path string) ([]Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var samples []Sample
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var s Sample
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, fmt.Errorf("parsing sample: %w", err)
		}
		samples = append(samples, s)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading samples: %w", err)
	}
	return samples, nil
}
