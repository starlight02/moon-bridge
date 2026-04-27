package deepseekv4

import (
	"encoding/json"
	"testing"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/openai"
)

func TestStripReasoningContentStripsField(t *testing.T) {
	input := json.RawMessage(`[{"role":"assistant","content":"hi","reasoning_content":"think"}]`)
	out := StripReasoningContent(input)
	if string(out) == string(input) {
		t.Fatal("expected reasoning_content to be stripped")
	}
	var items []map[string]any
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	if _, ok := items[0]["reasoning_content"]; ok {
		t.Fatal("reasoning_content should be removed")
	}
}

func TestStripReasoningContentLeavesStringInputAlone(t *testing.T) {
	input := json.RawMessage(`"hello"`)
	out := StripReasoningContent(input)
	if string(out) != `"hello"` {
		t.Fatalf("out = %s", out)
	}
}

func TestExtractReasoningContent(t *testing.T) {
	blocks := []anthropic.ContentBlock{
		{Type: "reasoning_content", Text: "Let me think..."},
		{Type: "text", Text: "Answer"},
	}
	got := ExtractReasoningContent(blocks)
	if got != "Let me think..." {
		t.Fatalf("got = %q", got)
	}
}

func TestInjectReasoningIntoOutput(t *testing.T) {
	output := []openai.OutputItem{
		{Type: "message", Role: "assistant", Content: []openai.ContentPart{{Type: "output_text", Text: "Answer"}}},
	}
	out := InjectReasoningIntoOutput(output, "Let me think...")
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0].Type != "message" || out[0].Content[0].Text != "Let me think..." {
		t.Fatalf("first item = %+v", out[0])
	}
}

func TestToAnthropicRequestClearsTemperatureAndTopP(t *testing.T) {
	f := 0.5
	req := anthropic.MessageRequest{Temperature: &f, TopP: &f, MaxTokens: 1000}
	ToAnthropicRequest(&req, nil)
	if req.Temperature != nil || req.TopP != nil {
		t.Fatal("expected temperature and top_p to be cleared")
	}
	if req.Thinking != nil {
		t.Fatalf("expected no thinking mapping, got %+v", req.Thinking)
	}
}

func TestToAnthropicRequestMapsReasoningEffort(t *testing.T) {
	for _, effort := range []string{"high", "max"} {
		req := anthropic.MessageRequest{Model: "deepseek-v4-pro"}
		ToAnthropicRequest(&req, map[string]any{"effort": effort})
		if req.Model != "deepseek-v4-pro" {
			t.Fatalf("Model = %q", req.Model)
		}
		if req.OutputConfig == nil || req.OutputConfig.Effort != effort {
			t.Fatalf("OutputConfig = %+v, want effort %q", req.OutputConfig, effort)
		}
		if req.Thinking != nil {
			t.Fatalf("expected no thinking mapping, got %+v", req.Thinking)
		}
	}
}

func TestToAnthropicRequestIgnoresUnsupportedReasoningEffort(t *testing.T) {
	req := anthropic.MessageRequest{Model: "deepseek-v4-pro"}
	ToAnthropicRequest(&req, map[string]any{"effort": "medium"})
	if req.OutputConfig != nil {
		t.Fatalf("OutputConfig = %+v, want nil", req.OutputConfig)
	}
}
