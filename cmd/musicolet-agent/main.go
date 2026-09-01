package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Ltre/MusicoletWeb/internal/agentclient"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println(agentclient.Version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	roots := filepath.SplitList(os.Getenv("MUSICOLET_AGENT_ROOTS"))
	if len(roots) == 1 && strings.Contains(roots[0], ",") {
		roots = strings.Split(roots[0], ",")
	}
	cfg := agentclient.Config{
		ServerURL: strings.TrimSpace(os.Getenv("MUSICOLET_SERVER_URL")),
		Token:     os.Getenv("MUSICOLET_AGENT_TOKEN"),
		Roots:     roots,
		AllowHTTP: envBool("MUSICOLET_AGENT_ALLOW_HTTP"),
	}
	logger := log.New(os.Stdout, "[MusicoletAgent] ", log.LstdFlags)
	if err := agentclient.Run(ctx, cfg, logger.Printf); err != nil && ctx.Err() == nil {
		logger.Fatal(err)
	}
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
