package config_test

import (
	"testing"

	"moonbridge/internal/config"
)

func TestLoadFromMapParsesDefaultsAndModelMap(t *testing.T) {
	cfg, err := config.LoadFromMap(map[string]string{
		"MOONBRIDGE_PROVIDER_BASE_URL": "https://provider.example.test",
		"MOONBRIDGE_PROVIDER_API_KEY":  "upstream-key",
		"MOONBRIDGE_MODEL_MAP":         "gpt-test=claude-test,gpt-fast=claude-fast",
		"MOONBRIDGE_CACHE_MODE":        "explicit",
		"MOONBRIDGE_CACHE_TTL":         "1h",
	})
	if err != nil {
		t.Fatalf("LoadFromMap() error = %v", err)
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

func TestLoadFromMapRequiresProviderSettings(t *testing.T) {
	_, err := config.LoadFromMap(map[string]string{})
	if err == nil {
		t.Fatal("LoadFromMap() error = nil, want missing provider settings error")
	}
}

func TestLoadFromMapRejectsInvalidCacheTTL(t *testing.T) {
	_, err := config.LoadFromMap(map[string]string{
		"MOONBRIDGE_PROVIDER_BASE_URL": "https://provider.example.test",
		"MOONBRIDGE_PROVIDER_API_KEY":  "upstream-key",
		"MOONBRIDGE_CACHE_TTL":         "24h",
	})
	if err == nil {
		t.Fatal("LoadFromMap() error = nil, want invalid cache TTL error")
	}
}
