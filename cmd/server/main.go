package main

import (
	"context"
	"fmt"
	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/app"
	"github.com/Ltre/MusicoletWeb/internal/auth"
	"github.com/Ltre/MusicoletWeb/internal/config"
	"github.com/Ltre/MusicoletWeb/internal/db"
	"github.com/Ltre/MusicoletWeb/internal/gitstore"
	"github.com/Ltre/MusicoletWeb/internal/httpapi"
	"github.com/Ltre/MusicoletWeb/internal/securestore"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, e := config.Load()
	if e != nil {
		log.Fatal(e)
	}
	if e = db.EnsureDirs(cfg.DataDir); e != nil {
		log.Fatal(e)
	}
	st, e := db.Open(cfg.DataDir)
	if e != nil {
		log.Fatal(e)
	}
	defer st.Close()
	gs, e := gitstore.Open(cfg.DataDir + string(os.PathSeparator) + "git" + string(os.PathSeparator) + "history.git")
	if e != nil {
		log.Fatal(e)
	}
	sec, e := securestore.New(st.DB, cfg.MasterKey)
	if e != nil {
		log.Fatal(e)
	}
	if e = sec.Bootstrap(context.Background(), "agent_token", cfg.AgentBootstrapToken); e != nil {
		log.Fatal(e)
	}
	svc := app.New(st, gs, cfg.DataDir)
	am := auth.New(cfg.AdminUsername, cfg.AdminPassword, cfg.TOTPSecret, cfg.SessionKey)
	hub := agenthub.New()
	api := httpapi.New(cfg, svc, am, hub, sec)
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", cfg.BindHost, cfg.Port), Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		log.Printf("MusicoletWeb listening on http://%s", srv.Addr)
		if e := srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
