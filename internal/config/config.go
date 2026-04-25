package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "config.yml"

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

type FileConfig struct {
	Server   ServerFileConfig   `yaml:"server"`
	Provider ProviderFileConfig `yaml:"provider"`
	Cache    CacheFileConfig    `yaml:"cache"`
}

type ServerFileConfig struct {
	Addr string `yaml:"addr"`
}

type ProviderFileConfig struct {
	BaseURL          string            `yaml:"base_url"`
	APIKey           string            `yaml:"api_key"`
	Version          string            `yaml:"version"`
	DefaultMaxTokens int               `yaml:"default_max_tokens"`
	Models           map[string]string `yaml:"models"`
}

type CacheFileConfig struct {
	Mode                     string `yaml:"mode"`
	TTL                      string `yaml:"ttl"`
	PromptCaching            *bool  `yaml:"prompt_caching"`
	AutomaticPromptCache     *bool  `yaml:"automatic_prompt_cache"`
	ExplicitCacheBreakpoints *bool  `yaml:"explicit_cache_breakpoints"`
	AllowRetentionDowngrade  *bool  `yaml:"allow_retention_downgrade"`
	MaxBreakpoints           int    `yaml:"max_breakpoints"`
	MinCacheTokens           int    `yaml:"min_cache_tokens"`
	ExpectedReuse            int    `yaml:"expected_reuse"`
	MinimumValueScore        int    `yaml:"minimum_value_score"`
}

func LoadFromEnv() (Config, error) {
	path := os.Getenv("MOONBRIDGE_CONFIG")
	if path == "" {
		path = DefaultConfigPath
	}
	return LoadFromFile(path)
}

func LoadFromFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return LoadFromYAML(data)
}

func LoadFromYAML(data []byte) (Config, error) {
	var fileConfig FileConfig
	if err := yaml.Unmarshal(data, &fileConfig); err != nil {
		return Config{}, err
	}
	return FromFileConfig(fileConfig)
}

func FromFileConfig(fileConfig FileConfig) (Config, error) {
	cfg := Config{
		Addr:             valueOrDefault(strings.TrimSpace(fileConfig.Server.Addr), ":8080"),
		ProviderBaseURL:  strings.TrimRight(strings.TrimSpace(fileConfig.Provider.BaseURL), "/"),
		ProviderAPIKey:   strings.TrimSpace(fileConfig.Provider.APIKey),
		ProviderVersion:  valueOrDefault(strings.TrimSpace(fileConfig.Provider.Version), "2023-06-01"),
		DefaultMaxTokens: intOrDefault(fileConfig.Provider.DefaultMaxTokens, 1024),
		ModelMap:         normalizeModelMap(fileConfig.Provider.Models),
		Cache: CacheConfig{
			Mode:                     valueOrDefault(strings.TrimSpace(fileConfig.Cache.Mode), "automatic"),
			TTL:                      valueOrDefault(strings.TrimSpace(fileConfig.Cache.TTL), "5m"),
			PromptCaching:            boolOrDefault(fileConfig.Cache.PromptCaching, true),
			AutomaticPromptCache:     boolOrDefault(fileConfig.Cache.AutomaticPromptCache, true),
			ExplicitCacheBreakpoints: boolOrDefault(fileConfig.Cache.ExplicitCacheBreakpoints, true),
			AllowRetentionDowngrade:  boolOrDefault(fileConfig.Cache.AllowRetentionDowngrade, false),
			MaxBreakpoints:           intOrDefault(fileConfig.Cache.MaxBreakpoints, 4),
			MinCacheTokens:           intOrDefault(fileConfig.Cache.MinCacheTokens, 1024),
			ExpectedReuse:            intOrDefault(fileConfig.Cache.ExpectedReuse, 2),
			MinimumValueScore:        intOrDefault(fileConfig.Cache.MinimumValueScore, 2048),
		},
	}

	if cfg.ProviderBaseURL == "" {
		return Config{}, errors.New("provider.base_url is required")
	}
	if cfg.ProviderAPIKey == "" {
		return Config{}, errors.New("provider.api_key is required")
	}
	if len(cfg.ModelMap) == 0 {
		return Config{}, errors.New("provider.models must contain at least one model mapping")
	}
	for alias, model := range cfg.ModelMap {
		if alias == "" || model == "" {
			return Config{}, errors.New("provider.models cannot contain empty aliases or models")
		}
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

func intOrDefault(value int, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizeModelMap(models map[string]string) map[string]string {
	if len(models) == 0 {
		return models
	}
	normalized := make(map[string]string, len(models))
	for alias, model := range models {
		normalized[strings.TrimSpace(alias)] = strings.TrimSpace(model)
	}
	return normalized
}
