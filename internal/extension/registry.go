package extension

import (
	"encoding/json"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/openai"
)

// Registry holds registered hooks and dispatches calls to enabled ones.
type Registry struct {
	hooks []Hook
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a hook to the registry.
func (r *Registry) Register(h Hook) {
	r.hooks = append(r.hooks, h)
}

// enabled returns hooks that are active for the given model.
func (r *Registry) enabled(model string) []Hook {
	if r == nil {
		return nil
	}
	var out []Hook
	for _, h := range r.hooks {
		if h.Enabled(model) {
			out = append(out, h)
		}
	}
	return out
}

// PreprocessInput chains PreprocessInput across all enabled hooks.
func (r *Registry) PreprocessInput(model string, raw json.RawMessage) json.RawMessage {
	for _, h := range r.enabled(model) {
		raw = h.PreprocessInput(raw)
	}
	return raw
}

// PostConvertRequest chains PostConvertRequest across all enabled hooks.
func (r *Registry) PostConvertRequest(model string, req *anthropic.MessageRequest, reasoning map[string]any) {
	for _, h := range r.enabled(model) {
		h.PostConvertRequest(req, reasoning)
	}
}

// PrependThinkingToMessages chains PrependThinkingToMessages.
func (r *Registry) PrependThinkingToMessages(model string, msgs []anthropic.Message, toolCallID string, sessionData map[string]any) []anthropic.Message {
	for _, h := range r.enabled(model) {
		msgs = h.PrependThinkingToMessages(msgs, toolCallID, sessionData[h.Name()])
	}
	return msgs
}

// PrependThinkingToAssistant chains PrependThinkingToAssistant.
func (r *Registry) PrependThinkingToAssistant(model string, blocks []anthropic.ContentBlock, sessionData map[string]any) []anthropic.ContentBlock {
	for _, h := range r.enabled(model) {
		blocks = h.PrependThinkingToAssistant(blocks, sessionData[h.Name()])
	}
	return blocks
}

// ExtractReasoningFromSummary returns the first non-empty result.
func (r *Registry) ExtractReasoningFromSummary(model string, summary []openai.ReasoningItemSummary) string {
	for _, h := range r.enabled(model) {
		if text := h.ExtractReasoningFromSummary(summary); text != "" {
			return text
		}
	}
	return ""
}

// OnResponseContent calls each enabled hook. Returns skip=true if any hook
// says skip, and concatenates all reasoning text.
func (r *Registry) OnResponseContent(model string, block anthropic.ContentBlock) (skip bool, reasoningText string) {
	for _, h := range r.enabled(model) {
		s, rt := h.OnResponseContent(block)
		if s {
			skip = true
		}
		reasoningText += rt
	}
	return
}

// PostConvertResponse chains PostConvertResponse.
func (r *Registry) PostConvertResponse(model string, resp *openai.Response, thinkingText string, hasToolCalls bool) {
	for _, h := range r.enabled(model) {
		h.PostConvertResponse(resp, thinkingText, hasToolCalls)
	}
}

// RememberResponseContent chains RememberResponseContent.
func (r *Registry) RememberResponseContent(model string, content []anthropic.ContentBlock, sessionData map[string]any) {
	for _, h := range r.enabled(model) {
		h.RememberResponseContent(content, sessionData[h.Name()])
	}
}

// NewStreamStates creates per-request stream state for all enabled hooks.
// Returns a map keyed by hook name.
func (r *Registry) NewStreamStates(model string) map[string]any {
	hooks := r.enabled(model)
	if len(hooks) == 0 {
		return nil
	}
	states := make(map[string]any, len(hooks))
	for _, h := range hooks {
		if s := h.NewStreamState(); s != nil {
			states[h.Name()] = s
		}
	}
	return states
}

// OnStreamBlockStart returns true if any enabled hook consumed the block.
func (r *Registry) OnStreamBlockStart(model string, index int, block *anthropic.ContentBlock, streamStates map[string]any) bool {
	for _, h := range r.enabled(model) {
		if h.OnStreamBlockStart(index, block, streamStates[h.Name()]) {
			return true
		}
	}
	return false
}

// OnStreamBlockDelta returns true if any enabled hook consumed the delta.
func (r *Registry) OnStreamBlockDelta(model string, index int, delta anthropic.StreamDelta, streamStates map[string]any) bool {
	for _, h := range r.enabled(model) {
		if h.OnStreamBlockDelta(index, delta, streamStates[h.Name()]) {
			return true
		}
	}
	return false
}

// OnStreamBlockStop returns (consumed, reasoningText) from the first hook
// that consumes the stop event.
func (r *Registry) OnStreamBlockStop(model string, index int, streamStates map[string]any) (bool, string) {
	for _, h := range r.enabled(model) {
		if consumed, text := h.OnStreamBlockStop(index, streamStates[h.Name()]); consumed {
			return true, text
		}
	}
	return false, ""
}

// OnStreamToolCall notifies all enabled hooks of a tool call.
func (r *Registry) OnStreamToolCall(model string, toolCallID string, streamStates map[string]any) {
	for _, h := range r.enabled(model) {
		h.OnStreamToolCall(toolCallID, streamStates[h.Name()])
	}
}

// OnStreamComplete notifies all enabled hooks that the stream is done.
func (r *Registry) OnStreamComplete(model string, streamStates map[string]any, outputText string, sessionData map[string]any) {
	for _, h := range r.enabled(model) {
		h.OnStreamComplete(streamStates[h.Name()], outputText, sessionData[h.Name()])
	}
}

// ResetStreamBlock notifies all enabled hooks to reset state for an index.
func (r *Registry) ResetStreamBlock(model string, index int, streamStates map[string]any) {
	for _, h := range r.enabled(model) {
		h.ResetStreamBlock(index, streamStates[h.Name()])
	}
}

// TransformError chains TransformError across all enabled hooks.
func (r *Registry) TransformError(model string, msg string) string {
	for _, h := range r.enabled(model) {
		msg = h.TransformError(msg)
	}
	return msg
}

// NewSessionData creates session data for all registered hooks (not just
// enabled ones, since enablement may vary per-request).
func (r *Registry) NewSessionData() map[string]any {
	if r == nil || len(r.hooks) == 0 {
		return nil
	}
	data := make(map[string]any, len(r.hooks))
	for _, h := range r.hooks {
		if d := h.NewSessionData(); d != nil {
			data[h.Name()] = d
		}
	}
	return data
}

// HasEnabled reports whether any hook is enabled for the given model.
func (r *Registry) HasEnabled(model string) bool {
	return len(r.enabled(model)) > 0
}
