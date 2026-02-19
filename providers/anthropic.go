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
)

// Anthropic implements the Provider interface for Anthropic's API.
// It translates OpenAI-format requests to Anthropic's format.
type Anthropic struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// anthropic request/response types
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Stream      bool               `json:"stream,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []anthropicContentBlock
}

type anthropicContentBlock struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
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
	Type         string                  `json:"type"`
	Index        int                     `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock  `json:"content_block,omitempty"`
	Delta        *anthropicDelta         `json:"delta,omitempty"`
	Message      *anthropicResponse      `json:"message,omitempty"`
	Usage        *anthropicStreamUsage   `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
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
	go a.readSSEStream(resp.Body, events, req.Model)

	return events, nil
}

func (a *Anthropic) readSSEStream(body io.ReadCloser, events chan<- StreamEvent, model string) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	var messageID string
	created := time.Now().Unix()

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
			}

		case "content_block_delta":
			if event.Delta != nil && event.Delta.Text != "" {
				chunk := &StreamChunk{
					ID:      messageID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index: 0,
							Delta: &Delta{
								Content: event.Delta.Text,
							},
						},
					},
				}
				events <- StreamEvent{Chunk: chunk}
			}

		case "message_delta":
			if event.Delta != nil && event.Delta.StopReason != "" {
				chunk := &StreamChunk{
					ID:      messageID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []Choice{
						{
							Index:        0,
							Delta:        &Delta{},
							FinishReason: a.translateStopReason(event.Delta.StopReason),
						},
					},
				}
				events <- StreamEvent{Chunk: chunk}
			}

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

	// extract system message and convert messages
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// anthropic uses a separate system field
			ar.System = a.extractContent(msg.Content)
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "assistant"
		} else if role == "user" || role == "tool" {
			role = "user"
		}

		ar.Messages = append(ar.Messages, anthropicMessage{
			Role:    role,
			Content: a.extractContent(msg.Content),
		})
	}

	return ar
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
	// join all text content blocks
	var content strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}

	return &ChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: &Message{
					Role:    "assistant",
					Content: content.String(),
				},
				FinishReason: a.translateStopReason(resp.StopReason),
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
		return NewAPIError(fmt.Sprintf("Anthropic API error: %s", string(body)), ErrorTypeServer)
	}
}
