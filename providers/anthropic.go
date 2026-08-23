package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/michaelquigley/df/dl"
)

// Anthropic implements the Provider interface for Anthropic's API.
// it translates OpenAI-format requests to Anthropic's format.
type Anthropic struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// anthropic request/response types
type anthropicRequest struct {
	Model         string               `json:"model"`
	MaxTokens     int                  `json:"max_tokens"`
	System        string               `json:"system,omitempty"`
	Messages      []anthropicMessage   `json:"messages"`
	Stream        bool                 `json:"stream,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool      `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice,omitempty"`
}

// anthropicTool is a function tool in Anthropic's request format. InputSchema
// holds the JSON schema (equivalent to OpenAI's function.parameters).
type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

// anthropicToolChoice is Anthropic's tool_choice object: {"type":"auto"},
// {"type":"any"}, or {"type":"tool","name":"X"}.
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

// anthropicContentBlock serves three roles: request tool_use blocks (built from
// assistant tool_calls), request tool_result blocks (built from tool messages),
// and response blocks (text or tool_use) read back from Anthropic. every
// optional field is omitempty so unused fields don't leak into the wire format.
type anthropicContentBlock struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
	// tool_use (assistant request blocks, and response blocks)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result (user request blocks)
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"` // string or []block; we emit a string
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []anthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// anthropic streaming event types
type anthropicStreamEvent struct {
	Type         string                 `json:"type"`
	Index        int                    `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicDelta        `json:"delta,omitempty"`
	Message      *anthropicResponse     `json:"message,omitempty"`
	Usage        *anthropicStreamUsage  `json:"usage,omitempty"`
	Error        *anthropicStreamError  `json:"error,omitempty"`
}

// anthropicStreamError is the payload of a streaming `type: "error"` event
// (e.g. overloaded_error, rate_limit_error mid-stream).
type anthropicStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicStreamUsage struct {
	OutputTokens int `json:"output_tokens,omitempty"`
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(apiKey, baseURL string) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  http.DefaultClient,
	}
}

// NewAnthropicWithClient creates a new Anthropic provider with a custom HTTP client.
func NewAnthropicWithClient(apiKey, baseURL string, client *http.Client) *Anthropic {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  client,
	}
}

func (a *Anthropic) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	anthropicReq := a.translateRequest(req)
	anthropicReq.Stream = false

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return a.translateResponse(&anthropicResp, req.Model), nil
}

func (a *Anthropic) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamEvent, error) {
	anthropicReq := a.translateRequest(req)
	anthropicReq.Stream = true

	body, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, a.parseError(resp.StatusCode, respBody)
	}

	events := make(chan StreamEvent, 10)
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	go a.readSSEStream(resp.Body, events, req.Model, includeUsage)

	return events, nil
}

// readSSEStream translates anthropic's SSE into OpenAI-style chunks. When includeUsage is
// set (stream_options.include_usage), it emits a final chunk carrying token usage after the
// last content chunk — anthropic reports input tokens in message_start and output tokens in
// message_delta, so both are accumulated as the stream runs.
func (a *Anthropic) readSSEStream(body io.ReadCloser, events chan<- StreamEvent, model string, includeUsage bool) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var messageID string
	var promptTokens, completionTokens int
	created := time.Now().Unix()

	// streaming tool-call state: anthropic interleaves text and tool_use blocks
	// in a single content-block index space, but OpenAI tool_call indices count
	// only tool calls. map anthropic block index -> OpenAI tool_call index so a
	// call's index stays stable across its argument fragments.
	toolIndexByBlock := map[int]int{}
	nextToolIndex := 0

	emit := func(delta *Delta) {
		events <- StreamEvent{Chunk: &StreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []Choice{{Index: 0, Delta: delta}},
		}}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			events <- StreamEvent{Err: fmt.Errorf("failed to parse event: %w", err)}
			return
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil {
				messageID = event.Message.ID
				promptTokens = event.Message.Usage.InputTokens
			}

		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				idx := nextToolIndex
				toolIndexByBlock[event.Index] = idx
				nextToolIndex++
				// opening tool chunk: index + id + type + name, empty arguments
				emit(&Delta{ToolCalls: []ToolCall{{
					Index:    &idx,
					ID:       event.ContentBlock.ID,
					Type:     "function",
					Function: FunctionCall{Name: event.ContentBlock.Name},
				}}})
			}

		case "content_block_delta":
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case "input_json_delta":
				idx, ok := toolIndexByBlock[event.Index]
				if !ok || event.Delta.PartialJSON == "" {
					continue
				}
				toolIdx := idx
				// argument fragment: index + arguments only
				emit(&Delta{ToolCalls: []ToolCall{{
					Index:    &toolIdx,
					Function: FunctionCall{Arguments: event.Delta.PartialJSON},
				}}})
			default:
				if event.Delta.Text != "" {
					emit(&Delta{Content: event.Delta.Text})
				}
			}

		case "message_delta":
			// anthropic reports the cumulative output token count on message_delta.
			if event.Usage != nil {
				completionTokens = event.Usage.OutputTokens
			}
			if event.Delta != nil && event.Delta.StopReason != "" {
				finishReason := a.translateStopReason(event.Delta.StopReason)
				chunk := &StreamChunk{
					ID:      messageID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index:        0,
							Delta:        &Delta{},
							FinishReason: &finishReason,
						},
					},
				}
				events <- StreamEvent{Chunk: chunk}
				// OpenAI emits usage in a separate final chunk (empty choices) when the
				// caller set stream_options.include_usage. Without this, a streamed call
				// carries no token counts and downstream metering cannot bill it.
				if includeUsage {
					events <- StreamEvent{Chunk: &StreamChunk{
						ID:      messageID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []Choice{},
						Usage: &Usage{
							PromptTokens:     promptTokens,
							CompletionTokens: completionTokens,
							TotalTokens:      promptTokens + completionTokens,
						},
					}}
				}
			}

		case "error":
			// surface a mid-stream upstream error as a stream error event so the
			// handler writes the OpenAI-shaped SSE error and closes cleanly.
			msg := "anthropic stream error"
			if event.Error != nil && event.Error.Message != "" {
				msg = fmt.Sprintf("anthropic stream error: %s", event.Error.Message)
			}
			events <- StreamEvent{Err: fmt.Errorf("%s", msg)}
			return

		case "message_stop":
			events <- StreamEvent{Done: true}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Err: fmt.Errorf("stream read error: %w", err)}
	}
}

func (a *Anthropic) ListModels(ctx context.Context) ([]Model, error) {
	// anthropic doesn't have a public models list endpoint, return static list.
	// current models listed first, then legacy models still available via the API.
	// see https://docs.anthropic.com/en/docs/about-claude/models/overview
	return []Model{
		// current models
		{ID: "claude-opus-4-6", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4-6", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Object: "model", OwnedBy: "anthropic"},
		// legacy models
		{ID: "claude-sonnet-4-5-20250929", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-opus-4-5-20251101", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-opus-4-1-20250805", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-sonnet-4-20250514", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-opus-4-20250514", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-3-7-sonnet-20250219", Object: "model", OwnedBy: "anthropic"},
		{ID: "claude-3-haiku-20240307", Object: "model", OwnedBy: "anthropic"},
	}, nil
}

func (a *Anthropic) translateRequest(req *ChatCompletionRequest) *anthropicRequest {
	ar := &anthropicRequest{
		Model:       req.Model,
		MaxTokens:   4096, // anthropic requires max_tokens
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}

	if req.MaxTokens != nil {
		ar.MaxTokens = *req.MaxTokens
	}

	// handle stop sequences
	if req.Stop != nil {
		switch v := req.Stop.(type) {
		case string:
			ar.StopSequences = []string{v}
		case []interface{}:
			for _, s := range v {
				if str, ok := s.(string); ok {
					ar.StopSequences = append(ar.StopSequences, str)
				}
			}
		}
	}

	// extract system message and convert messages. tool-role messages become
	// anthropic tool_result blocks; consecutive ones coalesce into a single
	// user message (anthropic requires tool_results grouped at the start of the
	// user turn that follows the assistant tool_use turn).
	var pending []anthropicContentBlock
	flush := func() {
		if len(pending) > 0 {
			ar.Messages = append(ar.Messages, anthropicMessage{Role: "user", Content: pending})
			pending = nil
		}
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			// anthropic uses a separate system field
			ar.System = a.extractContent(msg.Content)
		case "tool":
			pending = append(pending, anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   a.extractContent(msg.Content),
			})
		case "assistant":
			flush()
			ar.Messages = append(ar.Messages, a.convertAssistantMessage(msg))
		default: // "user"
			flush()
			ar.Messages = append(ar.Messages, anthropicMessage{
				Role:    "user",
				Content: a.extractContent(msg.Content),
			})
		}
	}
	flush()

	// translate tools and tool_choice. tool_choice "none" drops tools entirely
	// (anthropic has no direct equivalent).
	choice, dropTools := a.translateToolChoice(req.ToolChoice)
	if !dropTools && len(req.Tools) > 0 {
		ar.Tools = a.translateTools(req.Tools)
		ar.ToolChoice = choice
	}

	return ar
}

// convertAssistantMessage converts an OpenAI assistant message into an anthropic
// message. when the assistant made tool calls, its content becomes an array of
// blocks: an optional leading text block followed by one tool_use block per call.
func (a *Anthropic) convertAssistantMessage(msg Message) anthropicMessage {
	if len(msg.ToolCalls) == 0 {
		return anthropicMessage{Role: "assistant", Content: a.extractContent(msg.Content)}
	}

	var blocks []anthropicContentBlock
	if text := a.extractContent(msg.Content); text != "" {
		blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
	}
	for _, tc := range msg.ToolCalls {
		input := json.RawMessage(tc.Function.Arguments)
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, anthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	return anthropicMessage{Role: "assistant", Content: blocks}
}

// translateTools maps OpenAI function tools to anthropic tools. non-function
// tools are skipped; nil/empty parameters default to an empty object schema.
func (a *Anthropic) translateTools(tools []Tool) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		if t.Type != "" && t.Type != "function" {
			continue
		}
		schema := t.Function.Parameters
		if isEmptySchema(schema) {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: schema,
		})
	}
	return out
}

// isEmptySchema reports whether an OpenAI function.parameters value carries no
// usable schema (nil, an empty object, or an empty string).
func isEmptySchema(schema any) bool {
	switch v := schema.(type) {
	case nil:
		return true
	case map[string]interface{}:
		return len(v) == 0
	case string:
		return v == ""
	default:
		return false
	}
}

// translateToolChoice maps OpenAI tool_choice to anthropic's tool_choice object.
// it returns dropTools=true for "none", signalling that tools should be omitted
// from the request (anthropic has no direct "none" equivalent).
func (a *Anthropic) translateToolChoice(tc any) (choice *anthropicToolChoice, dropTools bool) {
	switch v := tc.(type) {
	case nil:
		return nil, false
	case string:
		switch v {
		case "auto":
			return &anthropicToolChoice{Type: "auto"}, false
		case "required":
			return &anthropicToolChoice{Type: "any"}, false
		case "none":
			return nil, true
		default:
			return nil, false
		}
	case map[string]interface{}:
		// {"type":"function","function":{"name":"X"}}
		if fn, ok := v["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				return &anthropicToolChoice{Type: "tool", Name: name}, false
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func (a *Anthropic) extractContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func (a *Anthropic) translateResponse(resp *anthropicResponse, model string) *ChatCompletionResponse {
	// collect text and tool_use blocks
	var content strings.Builder
	var toolCalls []ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
		case "tool_use":
			args := string(block.Input)
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:       block.ID,
				Type:     "function",
				Function: FunctionCall{Name: block.Name, Arguments: args},
			})
		}
	}

	msg := &Message{Role: "assistant"}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		// match OpenAI: content is null when only tool calls are present
		if content.Len() > 0 {
			msg.Content = content.String()
		} else {
			msg.Content = nil
		}
	} else {
		msg.Content = content.String()
	}

	finishReason := a.translateStopReason(resp.StopReason)
	return &ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: &finishReason,
			},
		},
		Usage: &Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}

func (a *Anthropic) translateStopReason(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return reason
	}
}

func (a *Anthropic) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		errType := ErrorTypeServer
		switch errResp.Error.Type {
		case "authentication_error":
			errType = ErrorTypeAuthentication
		case "rate_limit_error":
			errType = ErrorTypeRateLimit
		case "invalid_request_error":
			errType = ErrorTypeInvalidRequest
		case "not_found_error":
			errType = ErrorTypeNotFound
		}
		return NewAPIError(errResp.Error.Message, errType)
	}

	switch statusCode {
	case http.StatusUnauthorized:
		return NewAPIError("invalid API key", ErrorTypeAuthentication)
	case http.StatusTooManyRequests:
		return NewAPIError("rate limit exceeded", ErrorTypeRateLimit)
	case http.StatusNotFound:
		return NewAPIError("resource not found", ErrorTypeNotFound)
	default:
		// keep the native upstream body out of the client-facing error; log it
		// for diagnostics and return a generic OpenAI-shaped server error.
		dl.Errorf("anthropic API error (status %d): %s", statusCode, string(body))
		return NewAPIError(fmt.Sprintf("Anthropic API error (status %d)", statusCode), ErrorTypeServer)
	}
}
