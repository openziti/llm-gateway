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
// It can connect via HTTP or through a zrok share.
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
// Use this for zrok-based connections.
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

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			events <- StreamEvent{Err: fmt.Errorf("failed to parse chunk: %w", err)}
			return
		}

		events <- StreamEvent{Chunk: &chunk}
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

func (l *Local) parseError(statusCode int, body []byte) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return NewAPIError(errResp.Error, ErrorTypeServer)
	}

	switch statusCode {
	case http.StatusNotFound:
		return NewAPIError("model not found", ErrorTypeNotFound)
	case http.StatusServiceUnavailable:
		return NewAPIError("service unavailable", ErrorTypeServiceUnavailable)
	default:
		return NewAPIError(fmt.Sprintf("backend API error: %s", string(body)), ErrorTypeServer)
	}
}
