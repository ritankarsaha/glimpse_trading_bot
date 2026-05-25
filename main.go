package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/config"
	"github.com/ritankarsaha/glimpse-bot/kelly"
	"github.com/ritankarsaha/glimpse-bot/model"
	"github.com/ritankarsaha/glimpse-bot/tui"
	"github.com/ritankarsaha/glimpse-bot/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	tuiMode := flag.Bool("tui", false, "launch Bloomberg-style terminal UI instead of web server")
	tuiModel := flag.String("model", "gaussian", "forecast model: gaussian, skewed, uniform, or a JSON file path")
	tuiSigma := flag.Float64("sigma", 5.0, "Gaussian σ as %% of spot price (used by gaussian/skewed models)")
	tuiKelly := flag.Float64("kelly", 0.25, "Kelly fraction c (0 < c ≤ 1; 0.25 = quarter-Kelly)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[main] config error: %v", err)
	}

	client := api.NewGlimpseClient(cfg.Token)

	if *tuiMode {
		runTUI(client, cfg, *tuiModel, *tuiSigma, *tuiKelly)
		return
	}

	// ── Web server mode (existing behaviour) ──────────────────────────────
	if cfg.DryRun {
		log.Println("[main] DRY RUN mode is ON — no real trades will be placed")
	}
	if cfg.Token == "" {
		log.Println("[main] No JWT token configured — paste one in the UI before starting the bot")
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

func runTUI(client *api.GlimpseClient, cfg *config.Config, modelName string, sigma, kellyFrac float64) {
	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "No JWT token configured. Set GLIMPSE_TOKEN or add token to config.json.")
		os.Exit(1)
	}

	forecaster, err := model.FromName(modelName, sigma)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model error: %v\n", err)
		os.Exit(1)
	}

	kellyCfg := kelly.DefaultConfig()
	kellyCfg.KellyFraction = kellyFrac

	t := tui.New(client, tui.Config{
		TopicID:  cfg.TopicID,
		PollSec:  cfg.PollSec,
		DryRun:   cfg.DryRun,
		Model:    forecaster,
		KellyCfg: kellyCfg,
	})
	if err := t.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
