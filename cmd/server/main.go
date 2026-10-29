package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"milkgraph/internal/api"
	"milkgraph/internal/sse"
	"milkgraph/internal/store"
)

func main() {
	hc := flag.String("healthcheck", "", "probe path on local server (e.g. /readyz), exit 0 if 200")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if *hc != "" {
		bind := os.Getenv("BIND_ADDRESS")
		if bind == "" {
			bind = defaultBind
		}
		u, err := healthURL(bind, *hc)
		if err == nil {
			err = probe(context.Background(), u)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck gagal:", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Error("konfigurasi tidak valid", "error", err)
		os.Exit(2)
	}
	if err := run(cfg, log); err != nil {
		log.Error("server berhenti dengan error", "error", err)
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	db, err := store.Open(openCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("buka database: %w", err)
	}
	defer db.Close()

	hub := sse.New(db)
	srv := &api.Server{Mode: cfg.Mode, DB: db, Hub: hub, JWTSecret: cfg.JWTSecret,
		JWTIssuer: cfg.JWTIssuer, JWTAudience: cfg.JWTAudience, Origins: cfg.Origins}
	if err := srv.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	hub.Run(ctx) // Run returns immediately; it owns its goroutines

	// ponytail: no WriteTimeout; SSE streams are long-lived and the hub manages its own writes.
	// Per-route deadlines live in API middleware.
	hs := &http.Server{
		Addr:              cfg.BindAddress,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("server mulai", "addr", cfg.BindAddress, "mode", cfg.Mode, "cors_origins", len(cfg.Origins))

	select {
	case err := <-errc:
		hub.Close()
		return err
	case <-ctx.Done():
	}
	log.Info("shutdown dimulai")
	hub.Close() // end SSE streams first, otherwise Shutdown waits on them
	sctx, scancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer scancel()
	if err := hs.Shutdown(sctx); err != nil {
		_ = hs.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("shutdown selesai")
	return nil
}
