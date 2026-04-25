// Package websearchinjected extracts the "injected" web search mode into a
// self-contained extension. When enabled, the bridge injects tavily_search
// and firecrawl_fetch as function-type tools instead of relying on the
// upstream Anthropic provider's web_search_20250305 server tool.
//
// The extension:
//   - Provides tool definitions for the bridge to inject
//   - Wraps the Anthropic client with the Orchestrator for server-side search execution
package websearchinjected

import (
	"moonbridge/internal/anthropic"
	"moonbridge/internal/extensions/websearch"
)

// IsEnabled checks whether the injected web search extension should activate.
// cfg must expose WebSearchInjected() bool.
func IsEnabled(cfg interface{ WebSearchInjected() bool }) bool {
	return cfg.WebSearchInjected()
}

// InjectTools returns function-type tools to inject into the Anthropic request
// when the bridge encounters a web_search / web_search_preview tool from Codex.
// firecrawlKey may be empty; if so only tavily_search is returned.
func InjectTools(firecrawlKey string) []anthropic.Tool {
	tools := []anthropic.Tool{
		{
			Name:        "tavily_search",
			Description: "Search the web using Tavily. Returns search results with titles, URLs, and content snippets. Call this when you need up-to-date information from the internet.",
			InputSchema: tavilySearchSchema(),
		},
	}
	if firecrawlKey != "" {
		tools = append(tools, anthropic.Tool{
			Name:        "firecrawl_fetch",
			Description: "Fetch and extract the full content of a web page as clean markdown using Firecrawl. Use this when you need the complete text of a specific URL, such as a blog post or documentation page.",
			InputSchema: firecrawlFetchSchema(),
		})
	}
	return tools
}

// WrapProvider wraps an Anthropic client with the injected search orchestrator.
// The returned *websearch.Orchestrator implements the same CreateMessage /
// StreamMessage interface as *anthropic.Client.
func WrapProvider(client *anthropic.Client, tavilyKey, firecrawlKey string, maxRounds int) *websearch.Orchestrator {
	return websearch.NewInjectedOrchestrator(websearch.OrchestratorConfig{
		Anthropic:       client,
		TavilyKey:       tavilyKey,
		FirecrawlKey:    firecrawlKey,
		SearchMaxRounds: maxRounds,
	})
}

// tavilySearchSchema returns the JSON Schema for the tavily_search injected tool.
func tavilySearchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query (required).",
			},
			"search_depth": map[string]any{
				"type":        "string",
				"enum":        []string{"basic", "advanced"},
				"description": "Depth of search. Basic is faster; advanced provides more comprehensive results.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (1-20).",
				"default":     5,
			},
			"topic": map[string]any{
				"type":        "string",
				"enum":        []string{"general", "news", "finance"},
				"description": "Search topic category.",
			},
			"time_range": map[string]any{
				"type":        "string",
				"enum":        []string{"day", "week", "month", "year"},
				"description": "Time range filter for results.",
			},
			"include_answer": map[string]any{
				"type":        "boolean",
				"description": "Include an AI-generated answer summarizing the search results.",
			},
			"include_domains": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Only include results from these domains.",
			},
			"exclude_domains": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Exclude results from these domains.",
			},
		},
		"required": []string{"query"},
	}
}

// firecrawlFetchSchema returns the JSON Schema for the firecrawl_fetch injected tool.
func firecrawlFetchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL of the web page to fetch.",
			},
		},
		"required": []string{"url"},
	}
}
