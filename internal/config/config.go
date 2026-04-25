package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr             string
	ProviderBaseURL  string
	ProviderAPIKey   string
	ProviderVersion  string
	DefaultMaxTokens int
	ModelMap         map[string]string
	Cache            CacheConfig
}

type CacheConfig struct {
	Mode                     string
	TTL                      string
	PromptCaching            bool
	AutomaticPromptCache     bool
	ExplicitCacheBreakpoints bool
	AllowRetentionDowngrade  bool
	MaxBreakpoints           int
	MinCacheTokens           int
	ExpectedReuse            int
	MinimumValueScore        int
}

func LoadFromEnv() (Config, error) {
	return Load(os.Getenv)
}

func LoadFromMap(values map[string]string) (Config, error) {
	return Load(func(key string) string {
		return values[key]
	})
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:             valueOrDefault(getenv("MOONBRIDGE_ADDR"), ":8080"),
		ProviderBaseURL:  strings.TrimRight(getenv("MOONBRIDGE_PROVIDER_BASE_URL"), "/"),
		ProviderAPIKey:   getenv("MOONBRIDGE_PROVIDER_API_KEY"),
		ProviderVersion:  valueOrDefault(getenv("MOONBRIDGE_PROVIDER_VERSION"), "2023-06-01"),
		DefaultMaxTokens: intOrDefault(getenv("MOONBRIDGE_DEFAULT_MAX_TOKENS"), 1024),
		ModelMap:         parseModelMap(getenv("MOONBRIDGE_MODEL_MAP")),
		Cache: CacheConfig{
			Mode:                     valueOrDefault(getenv("MOONBRIDGE_CACHE_MODE"), "automatic"),
			TTL:                      valueOrDefault(getenv("MOONBRIDGE_CACHE_TTL"), "5m"),
			PromptCaching:            true,
			AutomaticPromptCache:     true,
			ExplicitCacheBreakpoints: true,
			MaxBreakpoints:           intOrDefault(getenv("MOONBRIDGE_CACHE_MAX_BREAKPOINTS"), 4),
			MinCacheTokens:           intOrDefault(getenv("MOONBRIDGE_CACHE_MIN_TOKENS"), 1024),
			ExpectedReuse:            intOrDefault(getenv("MOONBRIDGE_CACHE_EXPECTED_REUSE"), 2),
			MinimumValueScore:        intOrDefault(getenv("MOONBRIDGE_CACHE_MIN_VALUE_SCORE"), 2048),
		},
	}

	if cfg.ProviderBaseURL == "" {
		return Config{}, errors.New("MOONBRIDGE_PROVIDER_BASE_URL is required")
	}
	if cfg.ProviderAPIKey == "" {
		return Config{}, errors.New("MOONBRIDGE_PROVIDER_API_KEY is required")
	}
	if err := cfg.Cache.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (cfg Config) ModelFor(model string) string {
	if cfg.ModelMap == nil {
		return model
	}
	if mapped, ok := cfg.ModelMap[model]; ok && mapped != "" {
		return mapped
	}
	return model
}

func (cfg CacheConfig) Validate() error {
	switch cfg.Mode {
	case "", "off", "automatic", "explicit", "hybrid":
	default:
		return fmt.Errorf("invalid cache mode %q", cfg.Mode)
	}
	switch cfg.TTL {
	case "", "5m", "1h":
	default:
		return fmt.Errorf("invalid cache ttl %q", cfg.TTL)
	}
	return nil
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intOrDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseModelMap(value string) map[string]string {
	result := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		from := strings.TrimSpace(parts[0])
		to := strings.TrimSpace(parts[1])
		if from != "" && to != "" {
			result[from] = to
		}
	}
	return result
}
