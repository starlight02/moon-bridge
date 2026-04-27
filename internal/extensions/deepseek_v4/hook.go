package deepseekv4

import (
	"encoding/json"
	"strings"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/extension"
	"moonbridge/internal/openai"
)

const HookName = "deepseek_v4"

// EnabledFunc is called to determine if the hook is active for a model.
type EnabledFunc func(modelAlias string) bool

// DSHook implements extension.Hook for DeepSeek V4 thinking extensions.
type DSHook struct {
	extension.BaseHook
	isEnabled EnabledFunc
}

// NewHook creates a DeepSeek V4 extension hook.
func NewHook(isEnabled EnabledFunc) *DSHook {
	return &DSHook{isEnabled: isEnabled}
}

func (h *DSHook) Name() string              { return HookName }
func (h *DSHook) Enabled(model string) bool { return h.isEnabled(model) }

// --- Request path ---

func (h *DSHook) PreprocessInput(raw json.RawMessage) json.RawMessage {
	return StripReasoningContent(raw)
}

func (h *DSHook) PostConvertRequest(req *anthropic.MessageRequest, reasoning map[string]any) {
	ToAnthropicRequest(req, reasoning)
}

func (h *DSHook) PrependThinkingToMessages(
	messages []anthropic.Message, toolCallID string, sessionData any,
) []anthropic.Message {
	state, _ := sessionData.(*State)
	if state == nil {
		return messages
	}
	state.PrependCachedForToolUse(&messages, toolCallID)
	return messages
}

func (h *DSHook) PrependThinkingToAssistant(
	blocks []anthropic.ContentBlock, sessionData any,
) []anthropic.ContentBlock {
	state, _ := sessionData.(*State)
	if state == nil {
		return blocks
	}
	return state.PrependCachedForAssistantText(blocks)
}

func (h *DSHook) ExtractReasoningFromSummary(summary []openai.ReasoningItemSummary) string {
	if len(summary) > 0 {
		return summary[0].Text
	}
	return ""
}

// --- Response path ---

func (h *DSHook) OnResponseContent(block anthropic.ContentBlock) (skip bool, reasoningText string) {
	switch block.Type {
	case "thinking", "reasoning_content":
		return true, ExtractReasoningContent([]anthropic.ContentBlock{block})
	case "text":
		if IsReasoningContentBlock(&block) {
			return true, ""
		}
	}
	return false, ""
}

func (h *DSHook) PostConvertResponse(resp *openai.Response, thinkingText string, hasToolCalls bool) {
	if thinkingText != "" && hasToolCalls {
		resp.Output = InjectReasoningIntoOutput(resp.Output, thinkingText)
	}
}

func (h *DSHook) RememberResponseContent(content []anthropic.ContentBlock, sessionData any) {
	state, _ := sessionData.(*State)
	if state == nil {
		return
	}
	state.RememberFromContent(content)
}

// --- Streaming path ---

func (h *DSHook) NewStreamState() any {
	return NewStreamState()
}

func (h *DSHook) OnStreamBlockStart(index int, block *anthropic.ContentBlock, streamState any) bool {
	ss, _ := streamState.(*StreamState)
	if ss == nil {
		return false
	}
	return ss.Start(index, block)
}

func (h *DSHook) OnStreamBlockDelta(index int, delta anthropic.StreamDelta, streamState any) bool {
	ss, _ := streamState.(*StreamState)
	if ss == nil {
		return false
	}
	return ss.Delta(index, delta)
}

func (h *DSHook) OnStreamBlockStop(index int, streamState any) (bool, string) {
	ss, _ := streamState.(*StreamState)
	if ss == nil {
		return false, ""
	}
	if !ss.Stop(index) {
		return false, ""
	}
	return true, ss.CompletedThinkingText()
}

func (h *DSHook) OnStreamToolCall(toolCallID string, streamState any) {
	ss, _ := streamState.(*StreamState)
	if ss == nil {
		return
	}
	ss.RecordToolCall(toolCallID)
}

func (h *DSHook) OnStreamComplete(streamState any, outputText string, sessionData any) {
	ss, _ := streamState.(*StreamState)
	state, _ := sessionData.(*State)
	if ss == nil || state == nil {
		return
	}
	state.RememberStreamResult(ss, outputText)
}

func (h *DSHook) ResetStreamBlock(index int, streamState any) {
	ss, _ := streamState.(*StreamState)
	if ss == nil {
		return
	}
	ss.Reset(index)
}

// --- Error path ---

func (h *DSHook) TransformError(msg string) string {
	if isThinkingHistoryError(msg) {
		return "Missing required thinking blocks - ensure reasoning items are preserved in conversation history for tool-call turns."
	}
	return msg
}

func isThinkingHistoryError(msg string) bool {
	return strings.Contains(msg, "content[].thinking") && strings.Contains(msg, "thinking mode")
}

// --- Session path ---

func (h *DSHook) NewSessionData() any {
	return NewState()
}
