//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/bridge"
	"moonbridge/internal/cache"
	"moonbridge/internal/config"
	"moonbridge/internal/server"
)

func TestResponsesTextE2E(t *testing.T) {
	env := loadE2EEnv(t)
	handler := newE2EHandler(env)

	response := postResponses(t, handler, map[string]any{
		"model":             "e2e-model",
		"instructions":      "Reply briefly. Do not use Markdown.",
		"input":             "Reply with the words Moon Bridge e2e ok.",
		"max_output_tokens": 64,
	})

	if response["object"] != "response" {
		t.Fatalf("object = %v", response["object"])
	}
	if response["status"] == "failed" {
		t.Fatalf("response failed: %+v", response)
	}
	outputText, _ := response["output_text"].(string)
	if strings.TrimSpace(outputText) == "" {
		t.Fatalf("output_text is empty: %+v", response)
	}
}

func TestResponsesFunctionToolE2E(t *testing.T) {
	env := loadE2EEnv(t)
	handler := newE2EHandler(env)

	response := postResponses(t, handler, map[string]any{
		"model": "e2e-model",
		"input": "Use the lookup_weather tool for Paris. Do not answer directly.",
		"tools": []map[string]any{
			{
				"type":        "function",
				"name":        "lookup_weather",
				"description": "Look up weather for a city.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []string{"city"},
				},
			},
		},
		"tool_choice":       "required",
		"max_output_tokens": 128,
	})

	call := findFunctionCall(t, response, "lookup_weather")
	if strings.TrimSpace(call["call_id"].(string)) == "" {
		t.Fatalf("call_id is empty: %+v", call)
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(call["arguments"].(string)), &arguments); err != nil {
		t.Fatalf("arguments are not JSON: %v; call=%+v", err, call)
	}
	if strings.TrimSpace(arguments["city"].(string)) == "" {
		t.Fatalf("city argument is empty: %+v", arguments)
	}
}

func TestResponsesPromptCacheE2E(t *testing.T) {
	env := loadE2EEnv(t)
	if env["MOONBRIDGE_E2E_CACHE"] != "1" {
		t.Skip("set MOONBRIDGE_E2E_CACHE=1 in .env.test to run cache-costing e2e")
	}
	handler := newE2EHandlerWithCache(env, config.CacheConfig{
		Mode:                     "explicit",
		TTL:                      "5m",
		PromptCaching:            true,
		AutomaticPromptCache:     true,
		ExplicitCacheBreakpoints: true,
		MaxBreakpoints:           4,
		MinCacheTokens:           1,
		ExpectedReuse:            2,
		MinimumValueScore:        1,
	})

	longContext := strings.Repeat("Moon Bridge cache prefix stability sentence. ", 900)
	request := map[string]any{
		"model":             "e2e-model",
		"instructions":      longContext,
		"input":             "Answer with one short sentence.",
		"prompt_cache_key":  "moonbridge-e2e-cache",
		"max_output_tokens": 32,
	}

	first := postResponses(t, handler, request)
	second := postResponses(t, handler, request)

	if cachedTokens(second) == 0 && cacheCreationTokens(first) == 0 {
		t.Fatalf("no cache usage signals observed; first=%+v second=%+v", first["usage"], second["usage"])
	}
}

func newE2EHandler(env map[string]string) http.Handler {
	return newE2EHandlerWithCache(env, config.CacheConfig{Mode: "off"})
}

func newE2EHandlerWithCache(env map[string]string, cacheConfig config.CacheConfig) http.Handler {
	providerBaseURL := firstNonEmpty(env["MOONBRIDGE_PROVIDER_BASE_URL"], env["ANTHROPIC_MESSAGE_BASE_URL"])
	providerAPIKey := firstNonEmpty(env["MOONBRIDGE_PROVIDER_API_KEY"], env["ANTHROPIC_API_KEY"])
	providerModel := firstNonEmpty(env["MOONBRIDGE_E2E_MODEL"], env["ANTHROPIC_MODEL_NAME"])
	providerVersion := firstNonEmpty(env["MOONBRIDGE_PROVIDER_VERSION"], env["ANTHROPIC_VERSION"], "2023-06-01")

	cfg := config.Config{
		ProviderBaseURL:  providerBaseURL,
		ProviderAPIKey:   providerAPIKey,
		ProviderVersion:  providerVersion,
		DefaultMaxTokens: 128,
		ModelMap:         map[string]string{"e2e-model": providerModel},
		Cache:            cacheConfig,
	}

	return server.New(server.Config{
		Bridge: bridge.New(cfg, cache.NewMemoryRegistry()),
		Provider: anthropic.NewClient(anthropic.ClientConfig{
			BaseURL: providerBaseURL,
			APIKey:  providerAPIKey,
			Version: providerVersion,
		}),
	})
}

func postResponses(t *testing.T, handler http.Handler, payload map[string]any) map[string]any {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal payload error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/responses", bytes.NewReader(body))
	request.Header.Set("content-type", "application/json")
	handler.ServeHTTP(recorder, request)

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response is not JSON: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, response = %+v", recorder.Code, response)
	}
	return response
}

func findFunctionCall(t *testing.T, response map[string]any, name string) map[string]any {
	t.Helper()

	output, ok := response["output"].([]any)
	if !ok {
		t.Fatalf("output missing or invalid: %+v", response)
	}
	for _, item := range output {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["type"] == "function_call" && itemMap["name"] == name {
			return itemMap
		}
	}
	t.Fatalf("function_call %q not found in output: %+v", name, output)
	return nil
}

func cachedTokens(response map[string]any) int {
	usage, _ := response["usage"].(map[string]any)
	details, _ := usage["input_tokens_details"].(map[string]any)
	return numberAsInt(details["cached_tokens"])
}

func cacheCreationTokens(response map[string]any) int {
	metadata, _ := response["metadata"].(map[string]any)
	providerUsage, _ := metadata["provider_usage"].(map[string]any)
	return numberAsInt(providerUsage["cache_creation_input_tokens"])
}

func loadE2EEnv(t *testing.T) map[string]string {
	t.Helper()

	root := findProjectRoot(t)
	values, err := parseDotenv(filepath.Join(root, ".env.test"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip(".env.test not found")
	}
	if err != nil {
		t.Fatalf("parse .env.test error = %v", err)
	}
	for _, key := range []string{"ANTHROPIC_MESSAGE_BASE_URL", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL_NAME"} {
		if strings.TrimSpace(values[key]) == "" {
			t.Fatalf("%s is required in .env.test", key)
		}
	}
	return values
}

func findProjectRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func parseDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		values[key] = value
	}
	return values, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func numberAsInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
