package backtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func Reconcile(path string, topicID int, cutoff time.Time, outcomeBin int) error {
	samples, err := LoadSamples(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	changed := false
	for i := range samples {
		s := &samples[i]
		if s.TopicID == topicID && s.OutcomeBin == -1 && !s.Timestamp.After(cutoff) {
			s.OutcomeBin = outcomeBin
			changed = true
		}
	}
	if !changed {
		return nil
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, s := range samples {
		data, err := json.Marshal(s)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("marshalling sample: %w", err)
		}
		data = append(data, '\n')
		if _, err := w.Write(data); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("writing sample: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flushing temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing temp file: %w", err)
	}
	return os.Rename(tmp, path)
}
