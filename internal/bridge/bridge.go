package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"moonbridge/internal/anthropic"
	"moonbridge/internal/cache"
	"moonbridge/internal/config"
	"moonbridge/internal/openai"
)

type Bridge struct {
	cfg      config.Config
	registry *cache.MemoryRegistry
}

type RequestError struct {
	Status  int
	Message string
	Param   string
	Code    string
}

func (err *RequestError) Error() string {
	return err.Message
}

func New(cfg config.Config, registry *cache.MemoryRegistry) *Bridge {
	if registry == nil {
		registry = cache.NewMemoryRegistry()
	}
	return &Bridge{cfg: cfg, registry: registry}
}

func (bridge *Bridge) ToAnthropic(request openai.ResponsesRequest) (anthropic.MessageRequest, cache.CacheCreationPlan, error) {
	if request.Model == "" {
		return anthropic.MessageRequest{}, cache.CacheCreationPlan{}, invalidRequest("model is required", "model", "missing_required_parameter")
	}

	messages, system, err := bridge.convertInput(request.Input)
	if err != nil {
		return anthropic.MessageRequest{}, cache.CacheCreationPlan{}, err
	}
	if request.Instructions != "" {
		system = append([]anthropic.ContentBlock{{Type: "text", Text: request.Instructions}}, system...)
	}
	if len(messages) == 0 {
		messages = []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: ""}}}}
	}

	tools, err := bridge.convertTools(request.Tools)
	if err != nil {
		return anthropic.MessageRequest{}, cache.CacheCreationPlan{}, err
	}
	toolChoice, err := bridge.convertToolChoice(request.ToolChoice)
	if err != nil {
		return anthropic.MessageRequest{}, cache.CacheCreationPlan{}, err
	}

	maxTokens := request.MaxOutputTokens
	if maxTokens == 0 {
		maxTokens = bridge.cfg.DefaultMaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}

	converted := anthropic.MessageRequest{
		Model:         bridge.cfg.ModelFor(request.Model),
		MaxTokens:     maxTokens,
		System:        system,
		Messages:      messages,
		Tools:         tools,
		ToolChoice:    toolChoice,
		Temperature:   request.Temperature,
		TopP:          request.TopP,
		StopSequences: parseStopSequences(request.Stop),
		Stream:        request.Stream,
		Metadata:      request.Metadata,
	}

	plan, err := bridge.planCache(request, converted)
	if err != nil {
		return anthropic.MessageRequest{}, cache.CacheCreationPlan{}, err
	}
	bridge.injectCacheControl(&converted, plan)

	return converted, plan, nil
}

func (bridge *Bridge) FromAnthropic(response anthropic.MessageResponse, model string) openai.Response {
	return bridge.FromAnthropicWithPlan(response, model, cache.CacheCreationPlan{})
}

func (bridge *Bridge) FromAnthropicWithPlan(response anthropic.MessageResponse, model string, plan cache.CacheCreationPlan) openai.Response {
	if plan.LocalKey != "" {
		bridge.registry.UpdateFromUsage(plan.LocalKey, cache.UsageSignals{
			InputTokens:              response.Usage.InputTokens,
			CacheCreationInputTokens: response.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     response.Usage.CacheReadInputTokens,
		}, response.Usage.InputTokens)
	}

	output := make([]openai.OutputItem, 0, len(response.Content))
	var outputText strings.Builder
	messageContent := make([]openai.ContentPart, 0)

	for index, block := range response.Content {
		switch block.Type {
		case "text":
			part := openai.ContentPart{Type: "output_text", Text: block.Text}
			messageContent = append(messageContent, part)
			outputText.WriteString(block.Text)
		case "tool_use":
			if len(messageContent) > 0 {
				output = append(output, openai.OutputItem{
					Type:    "message",
					ID:      fmt.Sprintf("msg_item_%d", index),
					Status:  "completed",
					Role:    "assistant",
					Content: messageContent,
				})
				messageContent = nil
			}
			output = append(output, openai.OutputItem{
				Type:      "function_call",
				ID:        "fc_" + block.ID,
				CallID:    block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
				Status:    "completed",
			})
		}
	}
	if len(messageContent) > 0 {
		output = append(output, openai.OutputItem{
			Type:    "message",
			ID:      "msg_item_0",
			Status:  "completed",
			Role:    "assistant",
			Content: messageContent,
		})
	}

	status, incomplete := statusFromStopReason(response.StopReason)
	usage := normalizeUsage(response.Usage)

	metadata := map[string]any{
		"provider_message_id": response.ID,
	}
	if response.Usage.CacheCreationInputTokens > 0 || response.Usage.CacheReadInputTokens > 0 || response.Usage.CacheCreation != nil {
		metadata["provider_usage"] = response.Usage
	}

	return openai.Response{
		ID:                responseID(response.ID),
		Object:            "response",
		CreatedAt:         time.Now().Unix(),
		Status:            status,
		Model:             model,
		Output:            output,
		OutputText:        outputText.String(),
		Usage:             usage,
		Metadata:          metadata,
		IncompleteDetails: incomplete,
	}
}

func (bridge *Bridge) ErrorResponse(err error) (int, openai.ErrorResponse) {
	var requestError *RequestError
	if errors.As(err, &requestError) {
		return requestError.Status, openai.ErrorResponse{Error: openai.ErrorObject{
			Message: requestError.Message,
			Type:    "invalid_request_error",
			Param:   requestError.Param,
			Code:    requestError.Code,
		}}
	}
	if providerError, ok := anthropic.IsProviderError(err); ok {
		return providerError.OpenAIStatus(), openai.ErrorResponse{Error: openai.ErrorObject{
			Message: providerError.Error(),
			Type:    providerError.OpenAIType(),
			Code:    providerError.OpenAICode(),
		}}
	}
	return http.StatusInternalServerError, openai.ErrorResponse{Error: openai.ErrorObject{
		Message: err.Error(),
		Type:    "server_error",
		Code:    "internal_error",
	}}
}

func (bridge *Bridge) convertInput(raw json.RawMessage) ([]anthropic.Message, []anthropic.ContentBlock, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, nil, invalidRequest("input must be a string or array", "input", "invalid_request_error")
		}
		return []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: text}}}}, nil, nil
	}
	if !strings.HasPrefix(trimmed, "[") {
		return nil, nil, invalidRequest("input must be a string or array", "input", "invalid_request_error")
	}

	var items []inputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, nil, invalidRequest("input array is invalid", "input", "invalid_request_error")
	}

	messages := make([]anthropic.Message, 0, len(items))
	system := make([]anthropic.ContentBlock, 0)
	for _, item := range items {
		switch {
		case item.Type == "function_call_output":
			messages = append(messages, anthropic.Message{
				Role: "user",
				Content: []anthropic.ContentBlock{{
					Type:      "tool_result",
					ToolUseID: item.CallID,
					Content:   item.Output,
				}},
			})
		case item.Role == "system" || item.Role == "developer":
			system = append(system, contentBlocksFromRaw(item.Content)...)
		case item.Role == "assistant":
			messages = append(messages, anthropic.Message{Role: "assistant", Content: contentBlocksFromRaw(item.Content)})
		default:
			role := item.Role
			if role == "" {
				role = "user"
			}
			messages = append(messages, anthropic.Message{Role: role, Content: contentBlocksFromRaw(item.Content)})
		}
	}
	return messages, system, nil
}

func (bridge *Bridge) convertTools(tools []openai.Tool) ([]anthropic.Tool, error) {
	converted := make([]anthropic.Tool, 0, len(tools))
	for index, tool := range tools {
		if tool.Type != "function" {
			return nil, &RequestError{
				Status:  http.StatusBadRequest,
				Message: "Unsupported tool type: " + tool.Type,
				Param:   fmt.Sprintf("tools[%d].type", index),
				Code:    "unsupported_parameter",
			}
		}
		if tool.Parameters == nil {
			tool.Parameters = map[string]any{"type": "object"}
		}
		converted = append(converted, anthropic.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.Parameters,
		})
	}
	return converted, nil
}

func (bridge *Bridge) convertToolChoice(raw json.RawMessage) (anthropic.ToolChoice, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return anthropic.ToolChoice{}, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		switch value {
		case "auto", "none":
			return anthropic.ToolChoice{Type: value}, nil
		case "required":
			return anthropic.ToolChoice{Type: "any"}, nil
		default:
			return anthropic.ToolChoice{}, invalidRequest("unsupported tool_choice", "tool_choice", "unsupported_parameter")
		}
	}
	var object struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return anthropic.ToolChoice{}, invalidRequest("invalid tool_choice", "tool_choice", "invalid_request_error")
	}
	name := object.Name
	if name == "" {
		name = object.Function.Name
	}
	if name != "" {
		return anthropic.ToolChoice{Type: "tool", Name: name}, nil
	}
	return anthropic.ToolChoice{}, invalidRequest("unsupported tool_choice", "tool_choice", "unsupported_parameter")
}

func (bridge *Bridge) planCache(request openai.ResponsesRequest, converted anthropic.MessageRequest) (cache.CacheCreationPlan, error) {
	cfg := bridge.cfg.Cache
	if request.PromptCacheRetention == "24h" && !cfg.AllowRetentionDowngrade {
		return cache.CacheCreationPlan{}, &RequestError{
			Status:  http.StatusBadRequest,
			Message: "prompt_cache_retention 24h is not supported by Anthropic prompt caching",
			Param:   "prompt_cache_retention",
			Code:    "unsupported_parameter",
		}
	}

	ttl := cfg.TTL
	if request.PromptCacheRetention == "in_memory" {
		ttl = "5m"
	}
	if request.PromptCacheRetention == "24h" && cfg.AllowRetentionDowngrade {
		ttl = "1h"
	}

	toolsHash, _ := cache.CanonicalHash(converted.Tools)
	systemHash, _ := cache.CanonicalHash(converted.System)
	messagesHash, _ := cache.CanonicalHash(converted.Messages)
	planner := cache.NewPlannerWithRegistry(cache.PlannerConfig{
		Mode:              cfg.Mode,
		TTL:               ttl,
		PromptCaching:     cfg.PromptCaching,
		MaxBreakpoints:    cfg.MaxBreakpoints,
		MinCacheTokens:    cfg.MinCacheTokens,
		ExpectedReuse:     cfg.ExpectedReuse,
		MinimumValueScore: cfg.MinimumValueScore,
	}, bridge.registry)
	return planner.Plan(cache.PlanInput{
		ProviderID:        "anthropic",
		UpstreamAPIKeyID:  "configured-provider-key",
		Model:             converted.Model,
		PromptCacheKey:    request.PromptCacheKey,
		ToolsHash:         toolsHash,
		SystemHash:        systemHash,
		MessagePrefixHash: messagesHash,
		ToolCount:         len(converted.Tools),
		SystemBlockCount:  len(converted.System),
		MessageCount:      len(converted.Messages),
		EstimatedTokens:   estimateTokens(converted),
	})
}

func (bridge *Bridge) injectCacheControl(request *anthropic.MessageRequest, plan cache.CacheCreationPlan) {
	if plan.Mode == "off" {
		return
	}
	cacheControl := &anthropic.CacheControl{Type: "ephemeral"}
	if plan.TTL == "1h" {
		cacheControl.TTL = "1h"
	}
	if plan.Mode == "automatic" || plan.Mode == "hybrid" {
		request.CacheControl = cacheControl
	}
	for _, breakpointValue := range plan.Breakpoints {
		switch breakpointValue.Scope {
		case "tools":
			if len(request.Tools) > 0 {
				request.Tools[len(request.Tools)-1].CacheControl = cacheControl
			}
		case "system":
			if len(request.System) > 0 {
				request.System[len(request.System)-1].CacheControl = cacheControl
			}
		case "messages":
			if len(request.Messages) > 0 {
				messageIndex := len(request.Messages) - 1
				contentIndex := len(request.Messages[messageIndex].Content) - 1
				if contentIndex >= 0 {
					request.Messages[messageIndex].Content[contentIndex].CacheControl = cacheControl
				}
			}
		}
	}
}

type inputItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	CallID  string          `json:"call_id"`
	Output  string          `json:"output"`
}

func contentBlocksFromRaw(raw json.RawMessage) []anthropic.ContentBlock {
	if len(raw) == 0 || string(raw) == "null" {
		return []anthropic.ContentBlock{{Type: "text", Text: ""}}
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "\"") {
		var text string
		_ = json.Unmarshal(raw, &text)
		return []anthropic.ContentBlock{{Type: "text", Text: text}}
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil && len(parts) > 0 {
		blocks := make([]anthropic.ContentBlock, 0, len(parts))
		for _, part := range parts {
			if part.Type == "input_text" || part.Type == "text" || part.Type == "output_text" {
				blocks = append(blocks, anthropic.ContentBlock{Type: "text", Text: part.Text})
			}
		}
		if len(blocks) > 0 {
			return blocks
		}
	}
	return []anthropic.ContentBlock{{Type: "text", Text: trimmed}}
}

func parseStopSequences(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}
	}
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err == nil {
		return multiple
	}
	return nil
}

func statusFromStopReason(stopReason string) (string, *openai.IncompleteDetails) {
	switch stopReason {
	case "max_tokens":
		return "incomplete", &openai.IncompleteDetails{Reason: "max_output_tokens"}
	case "model_context_window":
		return "incomplete", &openai.IncompleteDetails{Reason: "max_input_tokens"}
	case "pause_turn":
		return "incomplete", &openai.IncompleteDetails{Reason: "provider_pause"}
	default:
		return "completed", nil
	}
}

func normalizeUsage(usage anthropic.Usage) openai.Usage {
	inputTokens := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	outputTokens := usage.OutputTokens
	return openai.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
		InputTokensDetails: openai.InputTokensDetails{
			CachedTokens: usage.CacheReadInputTokens,
		},
	}
}

func estimateTokens(request anthropic.MessageRequest) int {
	data, _ := json.Marshal(request)
	if len(data) == 0 {
		return 0
	}
	return len(data)/4 + 1
}

func responseID(providerID string) string {
	if providerID == "" {
		return "resp_generated"
	}
	if strings.HasPrefix(providerID, "resp_") {
		return providerID
	}
	return "resp_" + providerID
}

func invalidRequest(message, param, code string) error {
	return &RequestError{Status: http.StatusBadRequest, Message: message, Param: param, Code: code}
}
