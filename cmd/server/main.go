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

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/app"
	"github.com/Ltre/MusicoletWeb/internal/auth"
	"github.com/Ltre/MusicoletWeb/internal/config"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/httpapi"
	"github.com/Ltre/MusicoletWeb/internal/securestore"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	if err = db.EnsureDirs(cfg.DataDir); err != nil {
		log.Fatal(err)
	}
	st, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	gs, err := gitstore.Open(cfg.DataDir + string(os.PathSeparator) + "git" + string(os.PathSeparator) + "history.git")
	if err != nil {
		log.Fatal(err)
	}
	sec, err := securestore.New(st.DB, cfg.MasterKey)
	if err != nil {
		log.Fatal(err)
	}
	if err = sec.Bootstrap(context.Background(), "agent_token", cfg.AgentBootstrapToken); err != nil {
		log.Fatal(err)
	}

	svc := app.New(st, gs, cfg.DataDir)
	if err = svc.RecoverStartup(context.Background()); err != nil {
		log.Fatalf("startup recovery failed: %v", err)
	}

	am := auth.New(cfg.AdminUsername, cfg.AdminPassword, cfg.TOTPSecret, cfg.SessionKey)
	hub := agenthub.New()
	api := httpapi.New(cfg, svc, am, hub, sec)
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindHost, cfg.Port),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		log.Printf("MusicoletWeb listening on http://%s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
