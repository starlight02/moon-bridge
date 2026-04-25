package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"moonbridge/internal/logger"
	"sync"
	"time"
)

const (
	StateWarming      = "warming"
	StateWarm         = "warm"
	StateExpired      = "expired"
	StateNotCacheable = "not_cacheable"
	StateMissed       = "missed"
	StateFailed       = "failed"
)

type PlannerConfig struct {
	Mode                     string
	TTL                      string
	PromptCaching            bool
	AutomaticPromptCache     bool
	ExplicitCacheBreakpoints bool
	MaxBreakpoints           int
	MinCacheTokens           int
	ExpectedReuse            int
	MinimumValueScore        int
}

type PlanInput struct {
	ProviderID        string
	UpstreamWorkspace string
	UpstreamAPIKeyID  string
	Model             string
	PromptCacheKey    string
	ToolsHash         string
	SystemHash        string
	MessagePrefixHash string
	ToolCount         int
	SystemBlockCount  int
	MessageCount      int
	EstimatedTokens   int
}

type CacheCreationPlan struct {
	Mode        string
	TTL         string
	LocalKey    string
	Breakpoints []CacheBreakpoint
	WarmPolicy  string
	Reason      string
}

type CacheBreakpoint struct {
	Scope     string
	BlockPath string
	TTL       string
	Hash      string
}

type UsageSignals struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

type RegistryEntry struct {
	State                    string
	LocalKey                 string
	CreatedAt                time.Time
	ExpiresAt                time.Time
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	MissCount                int
}

type MemoryRegistry struct {
	mu      sync.Mutex
	entries map[string]RegistryEntry
}

type Planner struct {
	cfg      PlannerConfig
	registry *MemoryRegistry
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{entries: map[string]RegistryEntry{}}
}

func (registry *MemoryRegistry) Get(key string) (RegistryEntry, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	entry, ok := registry.entries[key]
	return entry, ok
}

func (registry *MemoryRegistry) Set(entry RegistryEntry) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.entries[entry.LocalKey] = entry
}

func (registry *MemoryRegistry) UpdateFromUsage(key string, usage UsageSignals, inputTokens int) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	entry := registry.entries[key]
	entry.LocalKey = key
	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}

	switch {
	case usage.CacheCreationInputTokens > 0:
		entry.State = StateWarm
		entry.CacheCreationInputTokens = usage.CacheCreationInputTokens
		entry.ExpiresAt = now.Add(5 * time.Minute)
	case usage.CacheReadInputTokens > 0:
		entry.State = StateWarm
		entry.CacheReadInputTokens = usage.CacheReadInputTokens
	case inputTokens <= 16:
		entry.State = StateNotCacheable
	default:
		entry.State = StateMissed
		entry.MissCount++
	}

	registry.entries[key] = entry
}

func NewPlanner(cfg PlannerConfig) *Planner {
	return NewPlannerWithRegistry(cfg, nil)
}

func NewPlannerWithRegistry(cfg PlannerConfig, registry *MemoryRegistry) *Planner {
	if cfg.Mode == "" {
		cfg.Mode = "automatic"
	}
	if cfg.TTL == "" {
		cfg.TTL = "5m"
	}
	if cfg.MaxBreakpoints == 0 {
		cfg.MaxBreakpoints = 4
	}
	if cfg.ExpectedReuse == 0 {
		cfg.ExpectedReuse = 1
	}
	return &Planner{cfg: cfg, registry: registry}
}

func (planner *Planner) Plan(input PlanInput) (CacheCreationPlan, error) {
	log := logger.L().With("model", input.Model)
	if !planner.cfg.PromptCaching || planner.cfg.Mode == "off" {
		log.Debug("cache disabled", "reason", "prompt_caching_disabled")
		return CacheCreationPlan{Mode: "off", TTL: planner.cfg.TTL, Reason: "prompt_caching_disabled"}, nil
	}
	if planner.cfg.MinCacheTokens > 0 && input.EstimatedTokens > 0 && input.EstimatedTokens < planner.cfg.MinCacheTokens {
		log.Debug("cache disabled", "reason", "below_min_cache_tokens", "estimated_tokens", input.EstimatedTokens, "min", planner.cfg.MinCacheTokens)
		return CacheCreationPlan{Mode: "off", TTL: planner.cfg.TTL, Reason: "below_min_cache_tokens"}, nil
	}
	if planner.cfg.MinimumValueScore > 0 && input.EstimatedTokens*planner.cfg.ExpectedReuse < planner.cfg.MinimumValueScore {
		log.Debug("cache disabled", "reason", "below_minimum_value_score", "estimated_tokens", input.EstimatedTokens, "expected_reuse", planner.cfg.ExpectedReuse)
		return CacheCreationPlan{Mode: "off", TTL: planner.cfg.TTL, Reason: "below_minimum_value_score"}, nil
	}

	useAutomatic := planner.cfg.AutomaticPromptCache && (planner.cfg.Mode == "automatic" || planner.cfg.Mode == "hybrid")
	useExplicit := planner.cfg.ExplicitCacheBreakpoints && (planner.cfg.Mode == "automatic" || planner.cfg.Mode == "explicit" || planner.cfg.Mode == "hybrid")
	if !useAutomatic && !useExplicit {
		return CacheCreationPlan{Mode: "off", TTL: planner.cfg.TTL, Reason: "cache_controls_disabled"}, nil
	}

	plan := CacheCreationPlan{
		Mode:       effectiveMode(useAutomatic, useExplicit),
		TTL:        planner.cfg.TTL,
		LocalKey:   localKey(input, planner.cfg.TTL),
		WarmPolicy: "none",
	}
	if planner.registry != nil {
		if entry, ok := planner.registry.Get(plan.LocalKey); ok && entry.State == StateWarm && (entry.ExpiresAt.IsZero() || entry.ExpiresAt.After(time.Now())) {
			plan.Reason = "registry_warm"
			log.Debug("cache registry warm", "key", plan.LocalKey)
		}
	}

	if useExplicit {
		plan.Breakpoints = planner.breakpoints(input)
		log.Debug("cache plan", "mode", plan.Mode, "breakpoints", len(plan.Breakpoints), "reason", plan.Reason)
		if len(plan.Breakpoints) == 0 {
			if useAutomatic {
				plan.Mode = "automatic"
				plan.Reason = "no_stable_breakpoints"
				return plan, nil
			}
			return CacheCreationPlan{
				Mode:     "off",
				TTL:      planner.cfg.TTL,
				LocalKey: plan.LocalKey,
				Reason:   "no_stable_breakpoints",
			}, nil
		}
	}
	log.Debug("cache plan", "mode", plan.Mode, "breakpoints", len(plan.Breakpoints), "reason", plan.Reason)
	return plan, nil
}

func effectiveMode(useAutomatic bool, useExplicit bool) string {
	switch {
	case useAutomatic && useExplicit:
		return "hybrid"
	case useAutomatic:
		return "automatic"
	case useExplicit:
		return "explicit"
	default:
		return "off"
	}
}

func (planner *Planner) breakpoints(input PlanInput) []CacheBreakpoint {
	maxBreakpoints := planner.cfg.MaxBreakpoints
	if maxBreakpoints <= 0 {
		maxBreakpoints = 4
	}
	breakpoints := make([]CacheBreakpoint, 0, maxBreakpoints)
	add := func(scope, path, hash string) {
		if len(breakpoints) >= maxBreakpoints || hash == "" {
			return
		}
		breakpoints = append(breakpoints, CacheBreakpoint{Scope: scope, BlockPath: path, TTL: planner.cfg.TTL, Hash: hash})
	}
	if input.ToolCount > 0 {
		add("tools", "tools["+itoa(input.ToolCount-1)+"]", input.ToolsHash)
	}
	if input.SystemBlockCount > 0 {
		add("system", "system["+itoa(input.SystemBlockCount-1)+"]", input.SystemHash)
	}
	if input.MessageCount > 0 {
		add("messages", "messages["+itoa(input.MessageCount-1)+"].content[last]", input.MessagePrefixHash)
	}
	return breakpoints
}

func CanonicalHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func localKey(input PlanInput, ttl string) string {
	parts := []string{
		input.ProviderID,
		input.UpstreamWorkspace,
		input.UpstreamAPIKeyID,
		input.Model,
		ttl,
		input.PromptCacheKey,
		input.ToolsHash,
		input.SystemHash,
		input.MessagePrefixHash,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
