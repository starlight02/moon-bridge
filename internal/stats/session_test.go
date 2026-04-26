package stats

import (
	"bytes"
	"strings"
	"testing"
)

func TestSummaryLogValueAlwaysIncludesCost(t *testing.T) {
	attrs := (Summary{}).LogValue().Group()
	for _, attr := range attrs {
		if attr.Key == "cost_cny" {
			return
		}
	}
	t.Fatalf("cost_cny missing from log attrs: %+v", attrs)
}

func TestWriteSummaryAlwaysIncludesTotalCost(t *testing.T) {
	var output bytes.Buffer

	WriteSummary(&output, Summary{})

	for _, want := range []string{
		"Summary：Session Cache Hit Rate(AVG): 0.0%, Billing: 0.00 CNY",
		"Total Cost:",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("summary missing %q: %s", want, output.String())
		}
	}
}

func TestFormatUsageLine(t *testing.T) {
	line := FormatUsageLine(UsageLineParams{
		RequestModel: "moonbridge",
		ActualModel:  "deepseek-v4-pro",
		Usage: Usage{
			InputTokens:              1_000_000,
			CacheCreationInputTokens: 500_000,
			CacheReadInputTokens:     500_000,
			OutputTokens:             250_000,
		},
		RequestCost:    6.789,
		TotalCost:      12.345,
		CacheHitRate:   25.00,
		CacheWriteRate: 25.00,
	})

	for _, want := range []string{
		"Model: moonbridge \u27a1\ufe0f deepseek-v4-pro",
		"0.5000 M Cache Read",
		"0.5000 M Cache Write",
		"1.0000 M Fresh",
		"Output: 0.2500 M",
		"Request Billing: 6.7890 CNY",
		"Total Billing: 12.3450 CNY",
		"Current Cache Hit Rate: 25.00%",
		"Current Cache Write Rate: 25.00%",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("usage line missing %q: %s", want, line)
		}
	}
}
