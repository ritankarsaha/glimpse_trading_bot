package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/backtest"
	"github.com/ritankarsaha/glimpse-bot/config"
	"github.com/ritankarsaha/glimpse-bot/kelly"
	"github.com/ritankarsaha/glimpse-bot/model"
	"github.com/ritankarsaha/glimpse-bot/tui"
	"github.com/ritankarsaha/glimpse-bot/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	tuiMode := flag.Bool("tui", false, "launch Bloomberg-style terminal UI instead of web server")
	tuiModel := flag.String("model", "", "forecast model: gaussian, skewed, uniform, lognormal, or a JSON/HTTP path (default from config)")
	tuiSigma := flag.Float64("sigma", 5.0, "Gaussian σ as %% of spot price (used by gaussian/skewed models)")
	tuiKelly := flag.Float64("kelly", 0.0, "Kelly fraction c (0 < c ≤ 1; 0.25 = quarter-Kelly; 0 = use config default)")
	tuiAnnualVol := flag.Float64("annual-vol", 0.0, "BTC annualised vol in %% (e.g. 65.0 for 65%%; 0 = use config default)")
	calibrate := flag.String("calibrate", "", "path to recorded forecast samples (JSONL); fits Benter calibration weights, writes the config calibration file, prints a report, and exits")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[main] config error: %v", err)
	}

	if *calibrate != "" {
		runCalibration(*calibrate, cfg)
		return
	}

	client := api.NewGlimpseClient(cfg.APIKey)

	if *tuiMode {
		modelName := *tuiModel
		if modelName == "" {
			modelName = cfg.ModelName
		}
		kellyFrac := *tuiKelly
		if kellyFrac <= 0 {
			kellyFrac = cfg.KellyFraction
		}
		annualVol := *tuiAnnualVol
		if annualVol <= 0 {
			annualVol = cfg.AnnualVolPct
		}
		runTUI(client, cfg, modelName, *tuiSigma, kellyFrac, annualVol)
		return
	}

	// ── Web server mode (existing behaviour) ──────────────────────────────
	if cfg.DryRun {
		log.Println("[main] DRY RUN mode is ON — no real trades will be placed")
	}
	if cfg.APIKey == "" {
		log.Println("[main] No API key configured — set GLIMPSE_API_KEY, add \"api_key\" to config.json, or paste one in the UI before starting the bot")
	}

	mux := http.NewServeMux()
	web.NewHandler(mux, client, cfg)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("[main] Shutdown signal received, draining…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[main] graceful shutdown error: %v", err)
		}
	}()

	log.Printf("[main] Dashboard at http://localhost%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[main] server error: %v", err)
	}
	log.Println("[main] Server stopped. Goodbye.")
}

func runTUI(client *api.GlimpseClient, cfg *config.Config, modelName string, sigma, kellyFrac, annualVol float64) {
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "No API key configured. Set GLIMPSE_API_KEY or add \"api_key\" to config.json.")
		os.Exit(1)
	}

	forecaster, err := model.FromNameWithCalibration(modelName, sigma, cfg.CalibrationFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model error: %v\n", err)
		os.Exit(1)
	}

	kellyCfg := kelly.DefaultConfig()
	kellyCfg.KellyFraction = kellyFrac
	if cfg.MinEdge > 0 {
		kellyCfg.MinEdge = cfg.MinEdge
	}
	if cfg.MaxBins > 0 {
		kellyCfg.MaxBins = cfg.MaxBins
	}

	t := tui.New(client, tui.Config{
		TopicID:      cfg.TopicID,
		PollSec:      cfg.PollSec,
		DryRun:       cfg.DryRun,
		Model:        forecaster,
		KellyCfg:     kellyCfg,
		AnnualVolPct: annualVol,
	})
	if err := t.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
func runCalibration(samplesPath string, cfg *config.Config) {
	samples, err := backtest.LoadSamples(samplesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading samples from %s: %v\n", samplesPath, err)
		os.Exit(1)
	}

	report, err := backtest.FitLogitWeights(samples, 0.2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fitting calibration weights: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Samples:        %d (train=%d, holdout=%d)\n", report.NSamples, report.NTrain, report.NHoldout)
	fmt.Printf("R2 (market):    %.4f\n", report.R2Market)
	fmt.Printf("R2 (combined):  %.4f\n", report.R2Combined)
	fmt.Printf("Delta R2:       %.4f\n", report.DeltaR2)
	fmt.Printf("Fitted weights: betaF=%.2f betaM=%.2f\n", report.BetaFundamental, report.BetaMarket)

	path := cfg.CalibrationFile
	if path == "" {
		path = "data/calibration.json"
	}
	weights := model.CalibrationWeights{BetaFundamental: report.BetaFundamental, BetaMarket: report.BetaMarket}
	data, err := json.MarshalIndent(weights, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encoding calibration weights: %v\n", err)
		os.Exit(1)
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "creating %s: %v\n", dir, err)
			os.Exit(1)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("Wrote calibration weights to %s\n", path)
}
