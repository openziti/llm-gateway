package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/keys"
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
		mux.HandleFunc("/metrics", providers.HandleMethodNotAllowed)
	}

	// the mux's built-in 404/405 responses are plain text; every client-visible
	// error stays OpenAI-shaped instead.
	mux.HandleFunc("/", providers.HandleNotFound)
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/health"} {
		mux.HandleFunc(path, providers.HandleMethodNotAllowed)
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

	if g.cfg.Tracing != nil && g.cfg.Tracing.Enabled {
		g.logRequest(&req)
	}

	// clear virtual model name to trigger semantic routing
	if req.Model == "auto" {
		req.Model = ""
	}

	keyEntry := keys.FromContext(ctx)

	capabilityResolved := false
	if routing.IsCapabilityModel(req.Model) {
		if g.semanticRouter == nil || !g.semanticRouter.Enabled() {
			providers.WriteError(w, providers.NewAPIError("capability resolution is not configured", providers.ErrorTypeInvalidRequest), http.StatusBadRequest)
			return
		}
		if !g.semanticRouter.AllowsExplicitModel() {
			providers.WriteError(w, providers.NewAPIError("capability models are disabled by routing.allow_explicit_model=false", providers.ErrorTypeInvalidRequest), http.StatusBadRequest)
			return
		}
		decision, err := g.semanticRouter.ResolveCapabilityModel(req.Model)
		if err != nil {
			providers.WriteError(w, providers.NewAPIError(err.Error(), providers.ErrorTypeInvalidRequest), http.StatusBadRequest)
			return
		}
		if g.logAndAuthorizeDecision(ctx, w, keyEntry, decision, &req) {
			return
		}
		capabilityResolved = true
	}

	// semantic routing: select model if not explicitly provided (or override if configured)
	if !capabilityResolved && g.semanticRouter != nil && g.semanticRouter.Enabled() {
		info := buildRequestInfo(&req)
		decision, err := g.semanticRouter.Route(ctx, info)
		if err != nil {
			dl.Errorf("semantic routing error: %v", err)
			// fall through to normal routing
		} else if decision.Model != "" {
			if g.logAndAuthorizeDecision(ctx, w, keyEntry, decision, &req) {
				return
			}
		}
	}

	if req.Model == "" {
		providers.WriteError(w, providers.ErrModelRequired, http.StatusBadRequest)
		return
	}

	if keyEntry != nil && !keyEntry.AllowsModel(req.Model) {
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

	keyName := ""
	if keyEntry != nil {
		keyName = keyEntry.Name
	}

	if req.Stream {
		g.handleStreamingCompletion(ctx, w, provider, providerType, keyName, &req)
	} else {
		g.handleNonStreamingCompletion(ctx, w, provider, providerType, keyName, &req)
	}

	if g.meters != nil {
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

// logAndAuthorizeDecision is the single owner of key->route authorization for
// both the capability and semantic-routing paths. it logs the decision line and
// the routing meter, then — for a decision that actually selected a route
// (Route != "", which excludes explicit-model passthrough) — denies with a 403
// when the key may not use that route, returning true. on success it binds the
// resolved model onto req and returns false.
func (g *Gateway) logAndAuthorizeDecision(ctx context.Context, w http.ResponseWriter, keyEntry *keys.Record, decision *routing.Decision, req *providers.ChatCompletionRequest) (denied bool) {
	keyName := ""
	if keyEntry != nil {
		keyName = keyEntry.Name
	}
	// log the decision before applying restrictions, so denied requests leave a
	// cascade trail too.
	dl.Infof("semantic routing: key='%s' method=%s route='%s' model='%s' confidence=%.2f latency=%dms cascade=[%s]",
		keyName, decision.Method, decision.Route, decision.Model, decision.Confidence, decision.LatencyMs, strings.Join(decision.Cascade, ","))
	if g.meters != nil {
		g.meters.routingDecisions.Add(ctx, 1, metric.WithAttributes(attribute.String("method", string(decision.Method))))
	}
	// route restrictions apply only to a decision that selected a route; an
	// explicit-model passthrough has no route (Route == "") and is governed by
	// the resolved-model check downstream.
	if keyEntry != nil && decision.Route != "" && !keyEntry.AllowsRoute(decision.Route) {
		dl.Infof("key '%s' denied access to route '%s'", keyEntry.Name, decision.Route)
		providers.WriteError(w,
			providers.NewAPIError(fmt.Sprintf("route '%s' is not allowed for this API key", decision.Route), providers.ErrorTypePermission),
			http.StatusForbidden,
		)
		return true
	}
	req.Model = decision.Model
	return false
}

func (g *Gateway) handleNonStreamingCompletion(ctx context.Context, w http.ResponseWriter, provider providers.Provider, providerType providers.ProviderType, keyName string, req *providers.ChatCompletionRequest) {
	resp, err := provider.ChatCompletion(ctx, req)
	if err != nil {
		g.writeProviderError(w, err)
		return
	}

	// the reported model is the gateway's binding, not the upstream's self-report
	// (a dated snapshot or server-side alias); log a differing upstream string so
	// provider-side aliasing stays visible rather than silently normalized.
	if resp.Model != "" && resp.Model != req.Model {
		dl.Infof("upstream reported '%s' for binding '%s'", resp.Model, req.Model)
	}
	resp.Model = req.Model
	if g.meters != nil && resp.Usage != nil {
		tokenAttrs := metric.WithAttributes(
			attribute.String("provider", string(providerType)),
			attribute.String("model", req.Model),
			attribute.String("key", keyName),
		)
		g.meters.tokensPrompt.Add(ctx, int64(resp.Usage.PromptTokens), tokenAttrs)
		g.meters.tokensCompletion.Add(ctx, int64(resp.Usage.CompletionTokens), tokenAttrs)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (g *Gateway) handleStreamingCompletion(ctx context.Context, w http.ResponseWriter, provider providers.Provider, providerType providers.ProviderType, keyName string, req *providers.ChatCompletionRequest) {
	sse := providers.NewSSEWriter(w)
	if sse == nil {
		providers.WriteError(w, providers.NewAPIError("streaming not supported", providers.ErrorTypeServer), http.StatusInternalServerError)
		return
	}

	// gate token capture behind the config toggle: request the upstream usage
	// chunk (OpenAI/local honor stream_options.include_usage; Anthropic always
	// streams usage) and record only when enabled, so token semantics stay
	// consistent across providers.
	captureUsage := g.cfg != nil && g.cfg.Metrics != nil && g.cfg.Metrics.StreamUsage
	if captureUsage {
		req.StreamOptions = &providers.StreamOptions{IncludeUsage: true}
	}

	events, err := provider.ChatCompletionStream(ctx, req)
	if err != nil {
		g.writeProviderError(w, err)
		return
	}

	sse.WriteHeaders()

	var usage *providers.Usage
	recordUsage := func() {
		if !captureUsage || usage == nil || g.meters == nil {
			if captureUsage && usage == nil {
				dl.Debugf("stream usage capture enabled but no usage reported (provider=%s model=%s)", providerType, req.Model)
			}
			return
		}
		tokenAttrs := metric.WithAttributes(
			attribute.String("provider", string(providerType)),
			attribute.String("model", req.Model),
			attribute.String("key", keyName),
		)
		g.meters.tokensPrompt.Add(ctx, int64(usage.PromptTokens), tokenAttrs)
		g.meters.tokensCompletion.Add(ctx, int64(usage.CompletionTokens), tokenAttrs)
	}

	loggedUpstreamModel := false
	for event := range events {
		if event.Err != nil {
			dl.Errorf("stream error: %v", event.Err)
			sse.WriteError(providers.ErrProviderError(event.Err.Error()))
			return
		}

		if event.Usage != nil {
			usage = event.Usage
		}

		if event.Done {
			recordUsage()
			sse.WriteDone()
			return
		}

		if event.Chunk != nil {
			// the reported model is the gateway's binding; log a differing
			// upstream self-report once per stream so aliasing stays visible.
			if !loggedUpstreamModel && event.Chunk.Model != "" && event.Chunk.Model != req.Model {
				dl.Infof("upstream reported '%s' for binding '%s'", event.Chunk.Model, req.Model)
				loggedUpstreamModel = true
			}
			event.Chunk.Model = req.Model
			if err := sse.WriteChunk(event.Chunk); err != nil {
				dl.Errorf("error writing chunk: %v", err)
				return
			}
		}
	}

	// the channel closed without a terminal Done or Err event; surface the
	// truncation rather than letting the client read it as completion.
	dl.Error("stream ended without a terminal event")
	sse.WriteError(providers.ErrProviderError("upstream stream ended unexpectedly"))
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

func (g *Gateway) logRequest(req *providers.ChatCompletionRequest) {
	maxLen := 200
	if g.cfg.Tracing.MaxContentLength > 0 {
		maxLen = g.cfg.Tracing.MaxContentLength
	}

	dl.Infof("trace: model='%s' messages=%d stream=%v tools=%d", req.Model, len(req.Messages), req.Stream, len(req.Tools))
	for i, msg := range req.Messages {
		content := extractMessageContent(msg.Content)
		if len(content) > maxLen {
			content = content[:maxLen] + "..."
		}
		// collapse newlines for single-line log output
		content = strings.ReplaceAll(content, "\n", "\\n")
		dl.Infof("trace:   [%d] role='%s' content='%s'", i, msg.Role, content)
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
