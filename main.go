package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ritankarsaha/glimpse-bot/api"
	"github.com/ritankarsaha/glimpse-bot/config"
	"github.com/ritankarsaha/glimpse-bot/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("[main] Glimpse Bot starting…")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[main] config error: %v", err)
	}

	if cfg.DryRun {
		log.Println("[main] DRY RUN mode is ON — no real trades will be placed")
	}
	if cfg.Token == "" {
		log.Println("[main] No JWT token configured — paste one in the UI before starting the bot")
	}

	client := api.NewGlimpseClient(cfg.Token)

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

	// Graceful shutdown on SIGINT / SIGTERM.
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
