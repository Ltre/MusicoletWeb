package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	DevAuthEnabled bool
	SessionTTL     time.Duration
}

func Load() (Config, error) {
	c := Config{
		BindHost:       strings.TrimSpace(env("MUSICOLET_BIND_HOST", "0.0.0.0")),
		DataDir:        strings.TrimSpace(env("MUSICOLET_DATA_DIR", "data")),
		AdminUsername:  strings.TrimSpace(os.Getenv("MUSICOLET_ADMIN_USERNAME")),
		AdminPassword:  os.Getenv("MUSICOLET_ADMIN_PASSWORD"),
		TOTPSecret:     strings.TrimSpace(os.Getenv("MUSICOLET_ADMIN_TOTP_SECRET")),
		SessionKey:     os.Getenv("MUSICOLET_SESSION_KEY"),
		AgentToken:     os.Getenv("MUSICOLET_AGENT_TOKEN"),
		PublicBaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv("MUSICOLET_PUBLIC_BASE_URL")), "/"),
		DevAuthEnabled: envBool("MUSICOLET_DEV_AUTH_ENABLED", false),
		SessionTTL:     12 * time.Hour,
	}
	port, err := strconv.Atoi(env("MUSICOLET_PORT", "4001"))
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("MUSICOLET_PORT must be an integer from 1 to 65535")
	}
	c.Port = port
	if ttl := strings.TrimSpace(os.Getenv("MUSICOLET_SESSION_TTL")); ttl != "" {
		c.SessionTTL, err = time.ParseDuration(ttl)
		if err != nil || c.SessionTTL < 5*time.Minute {
			return c, fmt.Errorf("MUSICOLET_SESSION_TTL must be at least 5m")
		}
	}
	if net.ParseIP(c.BindHost) == nil && c.BindHost != "localhost" {
		return c, fmt.Errorf("MUSICOLET_BIND_HOST must be an IP address or localhost")
	}
	if c.AdminUsername == "" || c.AdminPassword == "" {
		return c, errors.New("MUSICOLET_ADMIN_USERNAME and MUSICOLET_ADMIN_PASSWORD are required")
	}
	if len(c.AdminPassword) < 12 && !c.DevAuthEnabled {
		return c, errors.New("MUSICOLET_ADMIN_PASSWORD must contain at least 12 characters in production")
	}
	if c.TOTPSecret == "" && !c.DevAuthEnabled {
		return c, errors.New("MUSICOLET_ADMIN_TOTP_SECRET is required in production")
	}
	if len(c.SessionKey) < 32 {
		return c, errors.New("MUSICOLET_SESSION_KEY must contain at least 32 characters")
	}
	if len(c.AgentToken) < 24 && !c.DevAuthEnabled {
		return c, errors.New("MUSICOLET_AGENT_TOKEN must contain at least 24 characters in production")
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return c, fmt.Errorf("resolve data directory: %w", err)
	}
	c.DataDir = abs
	return c, nil
}

func (c Config) Address() string { return net.JoinHostPort(c.BindHost, strconv.Itoa(c.Port)) }

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
