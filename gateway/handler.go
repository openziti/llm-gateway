package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/providers"
	"github.com/openziti/llm-gateway/routing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (g *Gateway) newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", g.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", g.handleChatCompletions)
	mux.HandleFunc("GET /health", g.handleHealth)
	if g.metricsHandler != nil {
		mux.Handle("GET /metrics", g.metricsHandler)
	}
	if g.keyStore != nil {
		return g.keyStore.Middleware(mux)
	}
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

	if g.semanticRouter != nil && g.semanticRouter.Enabled() {
		allModels = append(allModels, providers.Model{
			ID: "auto", Object: "model", OwnedBy: "llm-gateway",
		})
	}

	resp := providers.ModelsResponse{
		Object: "list",
		Data:   allModels,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	if g.meters != nil {
		g.meters.inflight.Add(ctx, 1)
		defer g.meters.inflight.Add(ctx, -1)
	}

	var req providers.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		providers.WriteError(w, providers.ErrInvalidJSON, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		providers.WriteError(w, providers.ErrMessagesRequired, http.StatusBadRequest)
		return
	}

	// clear virtual model name to trigger semantic routing
	if req.Model == "auto" {
		req.Model = ""
	}

	keyEntry := KeyFromContext(ctx)

	// semantic routing: select model if not explicitly provided (or override if configured)
	if g.semanticRouter != nil && g.semanticRouter.Enabled() {
		info := buildRequestInfo(&req)
		decision, err := g.semanticRouter.Route(ctx, info)
		if err != nil {
			dl.Errorf("semantic routing error: %v", err)
			// fall through to normal routing
		} else if decision.Model != "" {
			if keyEntry != nil && !CheckRoute(keyEntry, decision.Route) {
				dl.Infof("key '%s' denied access to route '%s'", keyEntry.Name, decision.Route)
				providers.WriteError(w,
					providers.NewAPIError(fmt.Sprintf("route '%s' is not allowed for this API key", decision.Route), providers.ErrorTypePermission),
					http.StatusForbidden,
				)
				return
			}
			req.Model = decision.Model
			keyName := ""
			if keyEntry != nil {
				keyName = keyEntry.Name
			}
			dl.Infof("semantic routing: key='%s' method=%s route='%s' model='%s' confidence=%.2f latency=%dms cascade=[%s]",
				keyName, decision.Method, decision.Route, decision.Model, decision.Confidence, decision.LatencyMs, strings.Join(decision.Cascade, ","))
			if g.meters != nil {
				g.meters.routingDecisions.Add(ctx, 1, metric.WithAttributes(attribute.String("method", string(decision.Method))))
			}
		}
	}

	if req.Model == "" {
		providers.WriteError(w, providers.ErrModelRequired, http.StatusBadRequest)
		return
	}

	if keyEntry != nil && !CheckModel(keyEntry, req.Model) {
		dl.Infof("key '%s' denied access to model '%s'", keyEntry.Name, req.Model)
		providers.WriteError(w,
			providers.NewAPIError(fmt.Sprintf("model '%s' is not allowed for this API key", req.Model), providers.ErrorTypePermission),
			http.StatusForbidden,
		)
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

	streaming := "false"
	if req.Stream {
		streaming = "true"
	}

	if req.Stream {
		g.handleStreamingCompletion(ctx, w, provider, &req)
	} else {
		g.handleNonStreamingCompletion(ctx, w, provider, &req)
	}

	if g.meters != nil {
		keyName := ""
		if keyEntry != nil {
			keyName = keyEntry.Name
		}
		g.meters.requests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("provider", string(providerType)),
			attribute.String("model", req.Model),
			attribute.String("streaming", streaming),
			attribute.String("key", keyName),
		))
		g.meters.requestDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("provider", string(providerType)),
			attribute.String("model", req.Model),
			attribute.String("key", keyName),
		))
	}
}

func (g *Gateway) handleNonStreamingCompletion(ctx context.Context, w http.ResponseWriter, provider providers.Provider, req *providers.ChatCompletionRequest) {
	resp, err := provider.ChatCompletion(ctx, req)
	if err != nil {
		g.writeProviderError(w, err)
		return
	}

	if g.meters != nil && resp.Usage != nil {
		tokenAttrs := metric.WithAttributes(
			attribute.String("provider", resp.Model),
			attribute.String("model", req.Model),
		)
		g.meters.tokensPrompt.Add(ctx, int64(resp.Usage.PromptTokens), tokenAttrs)
		g.meters.tokensCompletion.Add(ctx, int64(resp.Usage.CompletionTokens), tokenAttrs)
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

func buildRequestInfo(req *providers.ChatCompletionRequest) *routing.RequestInfo {
	info := &routing.RequestInfo{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		HasTools:  len(req.Tools) > 0,
	}

	for _, msg := range req.Messages {
		content := extractMessageContent(msg.Content)
		info.Messages = append(info.Messages, routing.MessageInfo{
			Role:    msg.Role,
			Content: content,
		})
	}

	return info
}

// extractMessageContent extracts string content from a message's Content field,
// which may be a string or []ContentPart.
func extractMessageContent(content any) string {
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

func (g *Gateway) writeProviderError(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*providers.APIError); ok {
		if g.meters != nil {
			g.meters.providerErrors.Add(context.Background(), 1,
				metric.WithAttributes(attribute.String("error_type", apiErr.Type)),
			)
		}
		statusCode := providers.StatusCodeForError(apiErr.Type)
		providers.WriteError(w, apiErr, statusCode)
		return
	}

	if g.meters != nil {
		g.meters.providerErrors.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String("error_type", "unknown")),
		)
	}
	dl.Errorf("provider error: %v", err)
	apiErr := providers.ErrProviderError(err.Error())
	providers.WriteError(w, apiErr, http.StatusInternalServerError)
}
