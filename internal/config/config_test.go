package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsDotEnvAndEnvironmentOverridesIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(`
# Provider settings
APP_ADDR="0.0.0.0:9090"
AI_BASE_URL=https://provider.example/v1
AI_API_KEY='local-test-key'
AI_MODEL=test-model
AI_MAX_TOKENS=2048
`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"APP_ADDR", "APP_DB_PATH", "AI_BASE_URL", "AI_API_KEY", "AI_MODEL", "AI_MAX_TOKENS"} {
		t.Setenv(key, "")
	}
	t.Setenv("AI_MODEL", "environment-model")

	got := Load()
	if got.Addr != "0.0.0.0:9090" || got.AIBaseURL != "https://provider.example/v1" ||
		got.AIKey != "local-test-key" || got.AIModel != "environment-model" || got.AIMaxTokens != 2048 {
		t.Fatalf("Load() = %#v; did not apply .env settings and environment precedence", got)
	}
	if !got.AIEnabled() {
		t.Fatal("AIEnabled() = false with all provider settings configured")
	}
}

func TestAIEnabledRequiresCompleteProviderSettings(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{name: "missing base URL", cfg: Config{AIKey: "key", AIModel: "model"}},
		{name: "missing key", cfg: Config{AIBaseURL: "https://provider.example/v1", AIModel: "model"}},
		{name: "missing model", cfg: Config{AIBaseURL: "https://provider.example/v1", AIKey: "key"}},
		{name: "complete", cfg: Config{AIBaseURL: "https://provider.example/v1", AIKey: "key", AIModel: "model"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.AIEnabled(); got != tt.want {
				t.Fatalf("AIEnabled() = %t, want %t", got, tt.want)
			}
		})
	}
}
