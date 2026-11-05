package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds runtime configuration loaded from the environment.
type Config struct {
	Addr        string
	DBPath      string
	AIBaseURL   string
	AIKey       string
	AIModel     string
	AIMaxTokens int // output token cap (credit-saving); default 4096
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def, min, max int) int {
	raw := strings.TrimSpace(env(key, ""))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// Load reads configuration with sensible defaults.
func Load() Config {
	return Config{
		Addr:        env("APP_ADDR", "127.0.0.1:8080"),
		DBPath:      env("APP_DB_PATH", "./data/educore.db"),
		AIBaseURL:   env("AI_BASE_URL", ""),
		AIKey:       env("AI_API_KEY", ""),
		AIModel:     env("AI_MODEL", ""),
		AIMaxTokens: envInt("AI_MAX_TOKENS", 4096, 256, 8192),
	}
}

// AIEnabled reports whether real provider credentials are configured.
func (c Config) AIEnabled() bool {
	return c.AIModel != "" && c.AIKey != ""
}

func (c Config) String() string {
	s := fmt.Sprintf("addr=%s db=%s", c.Addr, c.DBPath)
	if c.AIEnabled() {
		s += " ai=enabled"
	} else {
		s += " ai=disabled (demo mode)"
	}
	return s
}
