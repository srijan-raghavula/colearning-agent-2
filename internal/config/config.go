package config

import (
	"fmt"
	"log"
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

func env(key, def string, fileEnv map[string]string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v := strings.TrimSpace(fileEnv[key]); v != "" {
		return v
	}
	return def
}

func envInt(key string, def, min, max int, fileEnv map[string]string) int {
	raw := strings.TrimSpace(env(key, "", fileEnv))
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
	fileEnv := loadDotEnv(".env")
	return Config{
		Addr:        env("APP_ADDR", "127.0.0.1:8080", fileEnv),
		DBPath:      env("APP_DB_PATH", "./data/educore.db", fileEnv),
		AIBaseURL:   env("AI_BASE_URL", "", fileEnv),
		AIKey:       env("AI_API_KEY", "", fileEnv),
		AIModel:     env("AI_MODEL", "", fileEnv),
		AIMaxTokens: envInt("AI_MAX_TOKENS", 4096, 256, 8192, fileEnv),
	}
}

func loadDotEnv(path string) map[string]string {
	fileEnv := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("read %s: %v", path, err)
		}
		return fileEnv
	}
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			log.Printf("ignore malformed %s line %d", path, lineNumber+1)
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		fileEnv[key] = value
	}
	return fileEnv
}

// AIEnabled reports whether all required provider settings are configured.
func (c Config) AIEnabled() bool {
	return strings.TrimSpace(c.AIBaseURL) != "" &&
		strings.TrimSpace(c.AIModel) != "" &&
		strings.TrimSpace(c.AIKey) != ""
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
