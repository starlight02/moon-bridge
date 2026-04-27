// Package extension defines the hook interface for bridge extensions.
//
// Extensions can intercept and modify requests and responses at well-defined
// points in the bridge pipeline. The bridge calls hooks in registration order;
// each hook receives the result of the previous hook (or the original value
// for the first hook).
//
// # Lifecycle
//
// A single request flows through these hooks in order:
//
//	                    ┌─────────────────────────────────────────────┐
//	                    │              Request path                   │
//	                    │                                             │
//	  OpenAI request ──►│ PreprocessInput                             │
//	                    │   ↓                                         │
//	                    │ PostConvertRequest                          │
//	                    │   ↓                                         │
//	                    │ ──── send to upstream ────                  │
//	                    │                                             │
//	                    │              Response path                  │
//	                    │                                             │
//	                    │ OnResponseContent (per content block)       │
//	                    │   ↓                                         │
//	                    │ PostConvertResponse                         │
//	                    │   ↓                                         │
//	                    │              Streaming path                 │
//	                    │                                             │
//	                    │ OnStreamBlockStart (per block)              │
//	                    │ OnStreamBlockDelta (per delta)              │
//	                    │ OnStreamBlockStop  (per block)              │
//	                    │   ↓                                         │
//	                    │              Error path                     │
//	                    │                                             │
//	                    │ TransformError                              │
//	                    │   ↓                                         │
//	                    │              Session path                   │
//	                    │                                             │
//	                    │ OnSessionCreate                             │
//	                    │ OnStreamComplete                            │
//	                    └─────────────────────────────────────────────┘
//
// # Extension state
//
// Extensions that need per-session state (e.g. thinking caches) should store
// it in session.ExtensionData via OnSessionCreate. Per-request streaming state
// should be returned from NewStreamState and passed through the stream hooks.
package extension

import (
	"encoding/json"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/openai"
)

// Hook is the interface that bridge extensions implement.
// All methods have default no-op behavior via embedding BaseHook.
type Hook interface {
	// Name returns a short identifier for logging.
	Name() string

	// Enabled reports whether this extension is active for the given model.
	Enabled(modelAlias string) bool

	// --- Request path ---

	// PreprocessInput transforms the raw OpenAI input JSON before it is
	// parsed into Anthropic messages. Use this to strip or rewrite fields
	// that the upstream provider cannot handle (e.g. reasoning_content).
	PreprocessInput(raw json.RawMessage) json.RawMessage

	// PostConvertRequest mutates the Anthropic request after the bridge has
	// built it from the OpenAI request. Use this to adjust parameters like
	// temperature, thinking config, or tool definitions.
	PostConvertRequest(req *anthropic.MessageRequest, reasoning map[string]any)

	// PrependThinkingToMessages is called during input conversion to let
	// extensions inject cached thinking blocks before tool_result messages.
	// Returns the messages slice (possibly with prepended thinking blocks)
	// and any pending reasoning text that was consumed.
	PrependThinkingToMessages(
		messages []anthropic.Message,
		toolCallID string,
		sessionData any,
	) []anthropic.Message

	// PrependThinkingToAssistant is called for assistant message blocks to
	// let extensions inject cached thinking blocks.
	PrependThinkingToAssistant(
		blocks []anthropic.ContentBlock,
		sessionData any,
	) []anthropic.ContentBlock

	// ExtractReasoningFromSummary extracts reasoning text from an OpenAI
	// reasoning summary item. Returns the extracted text (empty if not
	// applicable).
	ExtractReasoningFromSummary(summary []openai.ReasoningItemSummary) string

	// --- Response path ---

	// OnResponseContent is called for each content block in a non-streaming
	// response. Returns:
	//   - skip: true if the block should be excluded from output
	//   - reasoningText: any reasoning text extracted from this block
	OnResponseContent(block anthropic.ContentBlock) (skip bool, reasoningText string)

	// PostConvertResponse is called after the full response has been
	// converted. Use this to inject reasoning items, rewrite output, etc.
	PostConvertResponse(resp *openai.Response, thinkingText string, hasToolCalls bool)

	// RememberResponseContent is called with the full response content
	// blocks so extensions can cache thinking state for future turns.
	RememberResponseContent(content []anthropic.ContentBlock, sessionData any)

	// --- Streaming path ---

	// NewStreamState creates per-request streaming state. Returns nil if
	// the extension has no streaming state.
	NewStreamState() any

	// OnStreamBlockStart is called when a content_block_start event arrives.
	// Returns true if the extension consumed the block (bridge should skip
	// normal processing).
	OnStreamBlockStart(index int, block *anthropic.ContentBlock, streamState any) bool

	// OnStreamBlockDelta is called for each content_block_delta event.
	// Returns true if the extension consumed the delta.
	OnStreamBlockDelta(index int, delta anthropic.StreamDelta, streamState any) bool

	// OnStreamBlockStop is called when a content_block_stop event arrives.
	// Returns (consumed bool, reasoningText string). If consumed is true,
	// the bridge skips normal processing. reasoningText is any completed
	// reasoning to emit.
	OnStreamBlockStop(index int, streamState any) (consumed bool, reasoningText string)

	// OnStreamToolCall records a tool call ID in the stream state for
	// thinking cache association.
	OnStreamToolCall(toolCallID string, streamState any)

	// OnStreamComplete is called after the full stream has been processed.
	// Use this to persist streaming state back to the session.
	OnStreamComplete(streamState any, outputText string, sessionData any)

	// ResetStreamBlock is called when a stream block index is being reused.
	ResetStreamBlock(index int, streamState any)

	// --- Error path ---

	// TransformError rewrites a provider error message. Return the original
	// message if no transformation is needed.
	TransformError(msg string) string

	// --- Session path ---

	// NewSessionData creates per-session extension state. Called once when
	// a new session is created. The returned value is stored in
	// session.ExtensionData[name].
	NewSessionData() any
}

// BaseHook provides no-op defaults for all Hook methods.
// Extensions embed this and override only the methods they need.
type BaseHook struct{}

func (BaseHook) Name() string                          { return "" }
func (BaseHook) Enabled(string) bool                   { return false }
func (BaseHook) PreprocessInput(raw json.RawMessage) json.RawMessage { return raw }
func (BaseHook) PostConvertRequest(*anthropic.MessageRequest, map[string]any) {}
func (BaseHook) PrependThinkingToMessages(msgs []anthropic.Message, _ string, _ any) []anthropic.Message {
	return msgs
}
func (BaseHook) PrependThinkingToAssistant(blocks []anthropic.ContentBlock, _ any) []anthropic.ContentBlock {
	return blocks
}
func (BaseHook) ExtractReasoningFromSummary([]openai.ReasoningItemSummary) string { return "" }
func (BaseHook) OnResponseContent(anthropic.ContentBlock) (bool, string)          { return false, "" }
func (BaseHook) PostConvertResponse(*openai.Response, string, bool)               {}
func (BaseHook) RememberResponseContent([]anthropic.ContentBlock, any)            {}
func (BaseHook) NewStreamState() any                                              { return nil }
func (BaseHook) OnStreamBlockStart(int, *anthropic.ContentBlock, any) bool        { return false }
func (BaseHook) OnStreamBlockDelta(int, anthropic.StreamDelta, any) bool          { return false }
func (BaseHook) OnStreamBlockStop(int, any) (bool, string)                        { return false, "" }
func (BaseHook) OnStreamToolCall(string, any)                                     {}
func (BaseHook) OnStreamComplete(any, string, any)                                {}
func (BaseHook) ResetStreamBlock(int, any)                                        {}
func (BaseHook) TransformError(msg string) string                                 { return msg }
func (BaseHook) NewSessionData() any                                              { return nil }
