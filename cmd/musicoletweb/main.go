package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/config"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/media"
	webserver "github.com/Ltre/MusicoletWeb/internal/server"
	"github.com/Ltre/MusicoletWeb/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration rejected", "error", err)
		os.Exit(2)
	}
	if err = os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		logger.Error("create data directory", "error", err)
		os.Exit(1)
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "musicolet.db"))
	if err != nil {
		logger.Error("open SQLite", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	history, err := gitstore.Open(context.Background(), filepath.Join(cfg.DataDir, "history.git"))
	if err != nil {
		logger.Error("open Git history", "error", err)
		os.Exit(1)
	}
	var recordedHead string
	if scanErr := db.DB.QueryRow("SELECT commit_sha FROM git_commits ORDER BY revision DESC LIMIT 1").Scan(&recordedHead); scanErr == nil {
		if err = history.ForceRef(context.Background(), recordedHead); err != nil {
			logger.Error("reconcile Git history ref", "error", err)
			os.Exit(1)
		}
	}
	db.SetHistory(history)
	hub := agenthub.New(cfg.AgentToken)
	cache, err := media.NewCache(filepath.Join(cfg.DataDir, "cache", "media"), hub)
	if err != nil {
		logger.Error("open media cache", "error", err)
		os.Exit(1)
	}
	app := webserver.New(cfg, db, hub, cache, logger)
	httpServer := &http.Server{Addr: cfg.Address(), Handler: app.Handler(), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		logger.Info("MusicoletWeb listening", "address", cfg.Address(), "data", cfg.DataDir)
		if listenErr := httpServer.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", listenErr)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("MusicoletWeb stopped")
}
