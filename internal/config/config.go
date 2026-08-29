package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	BindHost       string
	Port           int
	DataDir        string
	AdminUsername  string
	AdminPassword  string
	TOTPSecret     string
	SessionKey     string
	AgentToken     string
	PublicBaseURL  string
	AgentAllowHTTP bool
}

func Load() (Config, error) {
	port := 4001
	if s := strings.TrimSpace(os.Getenv("MUSICOLET_PORT")); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > 65535 {
			return Config{}, errors.New("invalid MUSICOLET_PORT")
		}
		port = v
	}
	dataDir := strings.TrimSpace(os.Getenv("MUSICOLET_DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		BindHost: env("MUSICOLET_BIND_HOST", "0.0.0.0"), Port: port, DataDir: abs,
		AdminUsername: env("MUSICOLET_ADMIN_USERNAME", "admin"), AdminPassword: os.Getenv("MUSICOLET_ADMIN_PASSWORD"),
		TOTPSecret: strings.TrimSpace(os.Getenv("MUSICOLET_ADMIN_TOTP_SECRET")), SessionKey: os.Getenv("MUSICOLET_SESSION_KEY"),
		AgentToken: os.Getenv("MUSICOLET_AGENT_TOKEN"), PublicBaseURL: strings.TrimRight(os.Getenv("MUSICOLET_PUBLIC_BASE_URL"), "/"),
		AgentAllowHTTP: env("MUSICOLET_AGENT_ALLOW_HTTP", "0") == "1",
	}
	if c.AdminPassword == "" {
		return Config{}, errors.New("MUSICOLET_ADMIN_PASSWORD is required")
	}
	if c.TOTPSecret == "" {
		return Config{}, errors.New("MUSICOLET_ADMIN_TOTP_SECRET is required")
	}
	if c.SessionKey == "" {
		return Config{}, errors.New("MUSICOLET_SESSION_KEY is required")
	}
	if c.AgentToken == "" {
		return Config{}, errors.New("MUSICOLET_AGENT_TOKEN is required")
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return Config{}, err
	}
	return c, nil
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
