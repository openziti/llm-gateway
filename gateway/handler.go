package gateway

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/providers"
)

func (g *Gateway) newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", g.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", g.handleChatCompletions)
	return mux
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var allModels []providers.Model

	// collect models from all providers
	for pt, p := range g.providers {
		models, err := p.ListModels(ctx)
		if err != nil {
			dl.Errorf("error listing models from %s: %v", pt, err)
			continue
		}
		allModels = append(allModels, models...)
	}

	resp := providers.ModelsResponse{
		Object: "list",
		Data:   allModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req providers.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		providers.WriteError(w, providers.ErrInvalidJSON, http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		providers.WriteError(w, providers.ErrModelRequired, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		providers.WriteError(w, providers.ErrMessagesRequired, http.StatusBadRequest)
		return
	}

	provider, providerType, err := g.router.Route(req.Model)
	if err != nil {
		dl.Errorf("routing error for model '%s': %v", req.Model, err)
		apiErr := providers.ErrProviderNotConfigured(string(providerType))
		providers.WriteError(w, apiErr, http.StatusBadRequest)
		return
	}

	dl.Infof("routing model '%s' to %s", req.Model, providerType)

	if req.Stream {
		g.handleStreamingCompletion(ctx, w, provider, &req)
	} else {
		g.handleNonStreamingCompletion(ctx, w, provider, &req)
	}
}

func (g *Gateway) handleNonStreamingCompletion(ctx context.Context, w http.ResponseWriter, provider providers.Provider, req *providers.ChatCompletionRequest) {
	resp, err := provider.ChatCompletion(ctx, req)
	if err != nil {
		g.writeProviderError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleStreamingCompletion(ctx context.Context, w http.ResponseWriter, provider providers.Provider, req *providers.ChatCompletionRequest) {
	sse := providers.NewSSEWriter(w)
	if sse == nil {
		providers.WriteError(w, providers.NewAPIError("streaming not supported", providers.ErrorTypeServer), http.StatusInternalServerError)
		return
	}

	events, err := provider.ChatCompletionStream(ctx, req)
	if err != nil {
		g.writeProviderError(w, err)
		return
	}

	sse.WriteHeaders()

	for event := range events {
		if event.Err != nil {
			dl.Errorf("stream error: %v", event.Err)
			sse.WriteError(providers.ErrProviderError(event.Err.Error()))
			return
		}

		if event.Done {
			sse.WriteDone()
			return
		}

		if event.Chunk != nil {
			if err := sse.WriteChunk(event.Chunk); err != nil {
				dl.Errorf("error writing chunk: %v", err)
				return
			}
		}
	}
}

func (g *Gateway) writeProviderError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*providers.APIError); ok {
		statusCode := providers.StatusCodeForError(apiErr.Type)
		providers.WriteError(w, apiErr, statusCode)
		return
	}

	dl.Errorf("provider error: %v", err)
	apiErr := providers.ErrProviderError(err.Error())
	providers.WriteError(w, apiErr, http.StatusInternalServerError)
}
