package cache_test

import (
	"testing"

	"moonbridge/internal/cache"
)

func TestCanonicalHashIsStableAcrossMapOrder(t *testing.T) {
	first, err := cache.CanonicalHash(map[string]any{
		"b": 2,
		"a": map[string]any{"z": true, "c": "ok"},
	})
	if err != nil {
		t.Fatalf("CanonicalHash(first) error = %v", err)
	}

	second, err := cache.CanonicalHash(map[string]any{
		"a": map[string]any{"c": "ok", "z": true},
		"b": 2,
	})
	if err != nil {
		t.Fatalf("CanonicalHash(second) error = %v", err)
	}

	if first != second {
		t.Fatalf("hash mismatch: %s != %s", first, second)
	}
}

func TestPlannerCreatesExplicitBreakpointsInPrefixOrder(t *testing.T) {
	planner := cache.NewPlanner(cache.PlannerConfig{
		Mode:              "explicit",
		TTL:               "1h",
		PromptCaching:     true,
		MaxBreakpoints:    4,
		MinCacheTokens:    10,
		ExpectedReuse:     2,
		MinimumValueScore: 20,
	})

	plan, err := planner.Plan(cache.PlanInput{
		ProviderID:        "anthropic",
		UpstreamAPIKeyID:  "key-1",
		Model:             "claude-test",
		PromptCacheKey:    "tenant-docs",
		ToolsHash:         "tools-hash",
		SystemHash:        "system-hash",
		MessagePrefixHash: "messages-hash",
		ToolCount:         2,
		SystemBlockCount:  1,
		MessageCount:      3,
		EstimatedTokens:   1000,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if plan.Mode != "explicit" {
		t.Fatalf("Mode = %q", plan.Mode)
	}
	wantPaths := []string{"tools[1]", "system[0]", "messages[2].content[last]"}
	if len(plan.Breakpoints) != len(wantPaths) {
		t.Fatalf("breakpoints = %+v", plan.Breakpoints)
	}
	for index, want := range wantPaths {
		if got := plan.Breakpoints[index].BlockPath; got != want {
			t.Fatalf("breakpoint %d path = %q, want %q", index, got, want)
		}
	}
	if plan.LocalKey == "" {
		t.Fatal("LocalKey is empty")
	}
}

func TestPlannerSkipsShortPrefixes(t *testing.T) {
	planner := cache.NewPlanner(cache.PlannerConfig{
		Mode:           "automatic",
		TTL:            "5m",
		PromptCaching:  true,
		MinCacheTokens: 100,
	})

	plan, err := planner.Plan(cache.PlanInput{
		ProviderID:      "anthropic",
		Model:           "claude-test",
		EstimatedTokens: 20,
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Mode != "off" || plan.Reason != "below_min_cache_tokens" {
		t.Fatalf("Plan() = %+v", plan)
	}
}

func TestRegistryUpdatesFromUsageSignals(t *testing.T) {
	registry := cache.NewMemoryRegistry()

	registry.UpdateFromUsage("key", cache.UsageSignals{CacheCreationInputTokens: 1200}, 1200)
	entry, ok := registry.Get("key")
	if !ok || entry.State != cache.StateWarm {
		t.Fatalf("entry after creation = %+v, ok=%v", entry, ok)
	}

	registry.UpdateFromUsage("key", cache.UsageSignals{CacheReadInputTokens: 900}, 100)
	entry, ok = registry.Get("key")
	if !ok || entry.CacheReadInputTokens != 900 {
		t.Fatalf("entry after read = %+v, ok=%v", entry, ok)
	}

	registry.UpdateFromUsage("short", cache.UsageSignals{}, 5)
	entry, ok = registry.Get("short")
	if !ok || entry.State != cache.StateNotCacheable {
		t.Fatalf("short entry = %+v, ok=%v", entry, ok)
	}
}
