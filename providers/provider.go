package providers

import (
	"context"
)

// Provider defines the interface for LLM backend providers.
type Provider interface {
	// ChatCompletion sends a chat completion request and returns the response.
	ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)

	// ChatCompletionStream sends a streaming chat completion request.
	// Returns a channel that receives chunks and closes when complete.
	ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamEvent, error)

	// ListModels returns the list of available models from this provider.
	ListModels(ctx context.Context) ([]Model, error)
}

// StreamEvent represents an event in a streaming response.
type StreamEvent struct {
	Chunk *StreamChunk
	Err   error
	Done  bool
}

// Model represents an available model.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
