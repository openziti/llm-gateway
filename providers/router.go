package providers

import (
	"fmt"
	"strings"
)

// ProviderType identifies a provider backend.
type ProviderType string

const (
	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"
	ProviderLocal    ProviderType = "local"
)

// Router routes models to their appropriate providers.
type Router struct {
	providers map[ProviderType]Provider
}

// NewRouter creates a new Router with the given providers.
func NewRouter(providers map[ProviderType]Provider) *Router {
	return &Router{providers: providers}
}

// Route returns the provider for the given model name.
// Routing rules:
// - gpt-*, o1-*, o3-* -> OpenAI
// - claude-* -> Anthropic
// - everything else -> Local
func (r *Router) Route(model string) (Provider, ProviderType, error) {
	providerType := r.resolveProvider(model)

	provider, ok := r.providers[providerType]
	if !ok {
		return nil, "", fmt.Errorf("provider '%s' not configured for model '%s'", providerType, model)
	}

	return provider, providerType, nil
}

// resolveProvider determines which provider should handle the given model.
func (r *Router) resolveProvider(model string) ProviderType {
	lower := strings.ToLower(model)

	// openai models
	if strings.HasPrefix(lower, "gpt-") ||
		strings.HasPrefix(lower, "o1-") ||
		strings.HasPrefix(lower, "o3-") {
		return ProviderOpenAI
	}

	// anthropic models
	if strings.HasPrefix(lower, "claude-") {
		return ProviderAnthropic
	}

	// default to local for all other models (llama, mistral, etc.)
	return ProviderLocal
}

// GetProvider returns the provider for the given type.
func (r *Router) GetProvider(pt ProviderType) (Provider, bool) {
	p, ok := r.providers[pt]
	return p, ok
}

// ListAllModels returns models from all configured providers.
func (r *Router) ListAllModels() []Model {
	var allModels []Model
	for _, provider := range r.providers {
		if models, err := provider.ListModels(nil); err == nil {
			allModels = append(allModels, models...)
		}
	}
	return allModels
}
