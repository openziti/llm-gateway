package providers

import (
	"context"
	"testing"
)

// mockProvider is a minimal Provider implementation for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	return nil, nil
}

func (m *mockProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamEvent, error) {
	return nil, nil
}

func (m *mockProvider) ListModels(ctx context.Context) ([]Model, error) {
	return nil, nil
}

func TestRouterRoute(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	anthropic := &mockProvider{name: "anthropic"}
	ollama := &mockProvider{name: "ollama"}

	router := NewRouter(map[ProviderType]Provider{
		ProviderOpenAI:    openai,
		ProviderAnthropic: anthropic,
		ProviderOllama:    ollama,
	})

	tests := []struct {
		model        string
		wantProvider ProviderType
	}{
		// openai models
		{"gpt-4", ProviderOpenAI},
		{"gpt-4-turbo", ProviderOpenAI},
		{"gpt-3.5-turbo", ProviderOpenAI},
		{"GPT-4", ProviderOpenAI}, // case insensitive
		{"o1-preview", ProviderOpenAI},
		{"o1-mini", ProviderOpenAI},
		{"o3-mini", ProviderOpenAI},

		// anthropic models
		{"claude-3-opus-20240229", ProviderAnthropic},
		{"claude-3-sonnet-20240229", ProviderAnthropic},
		{"claude-3-haiku-20240307", ProviderAnthropic},
		{"claude-3-5-sonnet-20241022", ProviderAnthropic},
		{"CLAUDE-3-opus", ProviderAnthropic}, // case insensitive

		// ollama models (everything else)
		{"llama2", ProviderOllama},
		{"llama3", ProviderOllama},
		{"mistral", ProviderOllama},
		{"mixtral", ProviderOllama},
		{"codellama", ProviderOllama},
		{"phi3", ProviderOllama},
		{"qwen2", ProviderOllama},
		{"custom-model", ProviderOllama},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider, providerType, err := router.Route(tt.model)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if providerType != tt.wantProvider {
				t.Errorf("Route(%q) = %v, want %v", tt.model, providerType, tt.wantProvider)
			}
			if provider == nil {
				t.Errorf("Route(%q) returned nil provider", tt.model)
			}
		})
	}
}

func TestRouterRouteProviderNotConfigured(t *testing.T) {
	// router with only ollama configured
	router := NewRouter(map[ProviderType]Provider{
		ProviderOllama: &mockProvider{name: "ollama"},
	})

	// should fail for openai model
	_, _, err := router.Route("gpt-4")
	if err == nil {
		t.Error("expected error for unconfigured provider")
	}

	// should fail for anthropic model
	_, _, err = router.Route("claude-3-opus")
	if err == nil {
		t.Error("expected error for unconfigured provider")
	}

	// should succeed for ollama model
	provider, providerType, err := router.Route("llama2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if providerType != ProviderOllama {
		t.Errorf("expected ollama provider, got %v", providerType)
	}
	if provider == nil {
		t.Error("expected non-nil provider")
	}
}
