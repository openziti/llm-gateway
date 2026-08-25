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
)

// Local implements the Provider interface for any OpenAI-compatible backend.
// it can connect via HTTP or through a zrok share.
type Local struct {
	baseURL string
	client  *http.Client
}

// NewLocal creates a new local provider with direct HTTP access.
func NewLocal(baseURL string) *Local {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Local{
		baseURL: baseURL,
		client:  http.DefaultClient,
	}
}

// NewLocalWithClient creates a new local provider with a custom HTTP client.
// use this for zrok-based connections.
func NewLocalWithClient(baseURL string, client *http.Client) *Local {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Local{
		baseURL: baseURL,
		client:  client,
	}
}

func (l *Local) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, l.parseError(resp.StatusCode, respBody)
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

func (l *Local) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return nil, l.parseError(resp.StatusCode, respBody)
	}

	events := make(chan StreamEvent, 10)
	go l.readSSEStream(resp.Body, events)

	return events, nil
}

func (l *Local) readSSEStream(body io.ReadCloser, events chan<- StreamEvent) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			events <- StreamEvent{Done: true}
			return
		}

		chunk, err := parseStreamChunk(data)
		if err != nil {
			events <- StreamEvent{Err: err}
			return
		}

		// a terminal usage chunk (stream_options.include_usage) carries usage and
		// an empty choices array; surface it as a Usage event and don't also emit
		// it as an empty content chunk. Not all local backends honor include_usage,
		// in which case no usage event is ever produced.
		if usage := parseStreamUsage(data); usage != nil {
			events <- StreamEvent{Usage: usage}
			if len(chunk.Choices) == 0 {
				continue
			}
		}

		events <- StreamEvent{Chunk: chunk}
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Err: fmt.Errorf("stream read error: %w", err)}
	}
}

func (l *Local) ListModels(ctx context.Context) ([]Model, error) {
	// try the standard OpenAI-compatible endpoint first, so non-Ollama backends
	// (vLLM, llama-server, SGLang, etc.) work out of the box
	if models, err := l.listModelsOpenAI(ctx); err == nil {
		return models, nil
	}

	// fall back to Ollama's native /api/tags endpoint
	return l.listModelsLegacyTags(ctx)
}

func (l *Local) listModelsOpenAI(ctx context.Context) ([]Model, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", l.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data, nil
}

// listModelsLegacyTags uses Ollama's native /api/tags endpoint as a fallback
// for backends that don't implement the standard /v1/models endpoint.
func (l *Local) listModelsLegacyTags(ctx context.Context) ([]Model, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", l.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := l.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, l.parseError(resp.StatusCode, respBody)
	}

	var result struct {
		Models []struct {
			Name       string `json:"name"`
			ModifiedAt string `json:"modified_at"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]Model, 0, len(result.Models))
	for _, m := range result.Models {
		models = append(models, Model{
			ID:      m.Name,
			Object:  "model",
			OwnedBy: "ollama",
		})
	}

	return models, nil
}

// parseError maps a backend error body to an APIError. the OpenAI envelope is
// tried first: vLLM, SGLang, and llama-server all speak it, and reading their
// 400 as a server error would report a client mistake as a gateway failure.
// ollama's native {"error": "message"} form is the legacy fallback.
func (l *Local) parseError(statusCode int, body []byte) error {
	var openaiResp ErrorResponse
	if err := json.Unmarshal(body, &openaiResp); err == nil && openaiResp.Error.Message != "" {
		return &openaiResp.Error
	}

	var ollamaResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &ollamaResp); err == nil && ollamaResp.Error != "" {
		return NewAPIError(ollamaResp.Error, ErrorTypeServer)
	}

	// the body parsed as neither envelope. it belongs to an arbitrary backend
	// reached over an operator-configured transport, so its contents are not
	// safe to hand a client; report the status instead.
	switch statusCode {
	case http.StatusNotFound:
		return NewAPIError("model not found", ErrorTypeNotFound)
	case http.StatusServiceUnavailable:
		return NewAPIError("service unavailable", ErrorTypeServiceUnavailable)
	default:
		return NewAPIError(fmt.Sprintf("backend API error (HTTP %d)", statusCode), ErrorTypeServer)
	}
}
