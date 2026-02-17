package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EmbedClient is an HTTP client for embedding APIs (Ollama or OpenAI).
type EmbedClient struct {
	provider string
	model    string
	baseURL  string
	apiKey   string
	client   *http.Client
}

// NewEmbedClient creates a new EmbedClient.
func NewEmbedClient(provider, model, baseURL, apiKey string) *EmbedClient {
	return &EmbedClient{
		provider: provider,
		model:    model,
		baseURL:  baseURL,
		apiKey:   apiKey,
		client:   http.DefaultClient,
	}
}

// NewEmbedClientWithHTTPClient creates a new EmbedClient with a custom HTTP client.
func NewEmbedClientWithHTTPClient(provider, model, baseURL, apiKey string, client *http.Client) *EmbedClient {
	return &EmbedClient{
		provider: provider,
		model:    model,
		baseURL:  baseURL,
		apiKey:   apiKey,
		client:   client,
	}
}

// Embed generates an embedding for a single text.
func (c *EmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
	vectors, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return vectors[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
func (c *EmbedClient) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	switch c.provider {
	case "ollama":
		return c.embedOllama(ctx, texts)
	case "openai":
		return c.embedOpenAI(ctx, texts)
	default:
		return nil, fmt.Errorf("unsupported embedding provider '%s'", c.provider)
	}
}

// ollama embedding types

type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (c *EmbedClient) embedOllama(ctx context.Context, texts []string) ([][]float64, error) {
	reqBody := ollamaEmbedRequest{
		Model: c.model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed error %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return result.Embeddings, nil
}

// openai embedding types

type openaiEmbedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type openaiEmbedResponse struct {
	Data []openaiEmbedding `json:"data"`
}

type openaiEmbedding struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

func (c *EmbedClient) embedOpenAI(ctx context.Context, texts []string) ([][]float64, error) {
	reqBody := openaiEmbedRequest{
		Model: c.model,
		Input: texts,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embed error %d: %s", resp.StatusCode, string(respBody))
	}

	var result openaiEmbedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	vectors := make([][]float64, len(result.Data))
	for _, d := range result.Data {
		vectors[d.Index] = d.Embedding
	}

	return vectors, nil
}
