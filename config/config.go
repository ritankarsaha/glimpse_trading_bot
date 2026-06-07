package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Token         string `json:"token"`
	TopicID       int    `json:"topic_id"`
	Contracts     int    `json:"contracts"`
	Strategy      string `json:"strategy"`
	PollSec       int    `json:"poll_sec"`
	MaxTrades     int    `json:"max_trades"`
	DryRun        bool   `json:"dry_run"`
	Port          int    `json:"port"`
	MinExpiryMins int    `json:"min_expiry_mins"`
	MaxWalletPct  int    `json:"max_wallet_pct"`
	NoDuplicates  bool   `json:"no_duplicates"`

	// Model / Kelly parameters
	ModelName     string  `json:"model"`          // gaussian | skewed | uniform | lognormal | <url> | <path>
	AnnualVolPct  float64 `json:"annual_vol_pct"` // annualised BTC vol in %; e.g. 65.0
	KellyFraction float64 `json:"kelly_fraction"` // fractional Kelly, e.g. 0.25
	MinEdge       float64 `json:"min_edge"`       // minimum edge to trade, e.g. 0.005
	MaxBins       int     `json:"max_bins"`       // max bins per Kelly round
	ModelURL      string  `json:"model_url"`      // HTTP forecaster endpoint (overrides model name)
}

func Load() (*Config, error) {
	cfg := &Config{
		TopicID:       1116,
		Contracts:     1,
		Strategy:      "nearest",
		PollSec:       60,
		MaxTrades:     5,
		DryRun:        true,
		Port:          8080,
		MinExpiryMins: 10,
		MaxWalletPct:  10,
		NoDuplicates:  true,
		ModelName:     "gaussian",
		AnnualVolPct:  65.0,
		KellyFraction: 0.25,
		MinEdge:       0.005,
		MaxBins:       10,
	}

	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config.json: %w", err)
		}
	}

	if v := os.Getenv("GLIMPSE_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("GLIMPSE_TOPIC_ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TopicID = n
		}
	}
	if v := os.Getenv("GLIMPSE_CONTRACTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Contracts = n
		}
	}
	if v := os.Getenv("GLIMPSE_STRATEGY"); v != "" {
		cfg.Strategy = v
	}
	if v := os.Getenv("GLIMPSE_POLL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PollSec = n
		}
	}
	if v := os.Getenv("GLIMPSE_MAX_TRADES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxTrades = n
		}
	}
	if v := os.Getenv("GLIMPSE_DRY_RUN"); v != "" {
		cfg.DryRun = v == "true"
	}
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("GLIMPSE_MIN_EXPIRY_MINS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MinExpiryMins = n
		}
	}
	if v := os.Getenv("GLIMPSE_MAX_WALLET_PCT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxWalletPct = n
		}
	}
	if v := os.Getenv("GLIMPSE_NO_DUPLICATES"); v != "" {
		cfg.NoDuplicates = v == "true"
	}
	if v := os.Getenv("GLIMPSE_MODEL"); v != "" {
		cfg.ModelName = v
	}
	if v := os.Getenv("GLIMPSE_MODEL_URL"); v != "" {
		cfg.ModelURL = v
	}
	if v := os.Getenv("GLIMPSE_ANNUAL_VOL_PCT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.AnnualVolPct = f
		}
	}
	if v := os.Getenv("GLIMPSE_KELLY_FRACTION"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.KellyFraction = f
		}
	}
	if v := os.Getenv("GLIMPSE_MIN_EDGE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.MinEdge = f
		}
	}
	if v := os.Getenv("GLIMPSE_MAX_BINS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxBins = n
		}
	}

	return cfg, nil
}
