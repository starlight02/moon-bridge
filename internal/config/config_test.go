package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"moonbridge/internal/config"
)

func TestLoadFromYAMLParsesDefaultsAndModelMap(t *testing.T) {
	cfg, err := config.LoadFromYAML([]byte(`
provider:
  base_url: https://provider.example.test
  api_key: upstream-key
  models:
    gpt-test: claude-test
    gpt-fast: claude-fast
cache:
  mode: explicit
  ttl: 1h
`))
	if err != nil {
		t.Fatalf("LoadFromYAML() error = %v", err)
	}

	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.ProviderVersion != "2023-06-01" {
		t.Fatalf("ProviderVersion = %q", cfg.ProviderVersion)
	}
	if cfg.DefaultMaxTokens != 1024 {
		t.Fatalf("DefaultMaxTokens = %d", cfg.DefaultMaxTokens)
	}
	if got := cfg.ModelFor("gpt-test"); got != "claude-test" {
		t.Fatalf("ModelFor(gpt-test) = %q", got)
	}
	if cfg.Cache.Mode != "explicit" || cfg.Cache.TTL != "1h" {
		t.Fatalf("Cache = %+v", cfg.Cache)
	}
}

func TestLoadFromYAMLRequiresProviderSettings(t *testing.T) {
	_, err := config.LoadFromYAML([]byte(`{}`))
	if err == nil {
		t.Fatal("LoadFromYAML() error = nil, want missing provider settings error")
	}
}

func TestLoadFromYAMLRejectsInvalidCacheTTL(t *testing.T) {
	_, err := config.LoadFromYAML([]byte(`
provider:
  base_url: https://provider.example.test
  api_key: upstream-key
  models:
    gpt-test: claude-test
cache:
  ttl: 24h
`))
	if err == nil {
		t.Fatal("LoadFromYAML() error = nil, want invalid cache TTL error")
	}
}

func TestLoadFromYAMLRejectsEmptyModelMapping(t *testing.T) {
	_, err := config.LoadFromYAML([]byte(`
provider:
  base_url: https://provider.example.test
  api_key: upstream-key
  models:
    moonbridge: ""
`))
	if err == nil {
		t.Fatal("LoadFromYAML() error = nil, want empty model mapping error")
	}
}

func TestLoadFromEnvUsesMoonBridgeConfigPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(`
server:
  addr: 127.0.0.1:9999
provider:
  base_url: https://provider.example.test
  api_key: upstream-key
  models:
    moonbridge: claude-test
cache:
  mode: off
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("MOONBRIDGE_CONFIG", path)
	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" {
		t.Fatalf("Addr = %q", cfg.Addr)
	}
	if cfg.Cache.Mode != "off" {
		t.Fatalf("Cache.Mode = %q", cfg.Cache.Mode)
	}
}
