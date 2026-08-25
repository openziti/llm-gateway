package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openziti/llm-gateway/keys"
	"github.com/openziti/llm-gateway/providers"
	"github.com/openziti/llm-gateway/routing"
)

func mustKeyStore(t *testing.T, entries []keys.EntryConfig) *keys.Store {
	t.Helper()
	store, err := keys.NewStore(entries)
	if err != nil {
		t.Fatalf("keys.NewStore() = %v, want nil", err)
	}
	return store
}

// stubStreamProvider replays a fixed sequence of stream events and then closes
// the channel, letting tests exercise terminal and non-terminal stream shapes.
type stubStreamProvider struct {
	events []providers.StreamEvent
	gotReq *providers.ChatCompletionRequest
}

func (p *stubStreamProvider) ChatCompletion(_ context.Context, _ *providers.ChatCompletionRequest) (*providers.ChatCompletionResponse, error) {
	return nil, providers.ErrProviderError("not implemented")
}

func (p *stubStreamProvider) ChatCompletionStream(_ context.Context, req *providers.ChatCompletionRequest) (<-chan providers.StreamEvent, error) {
	p.gotReq = req
	ch := make(chan providers.StreamEvent, len(p.events))
	for _, ev := range p.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (p *stubStreamProvider) ListModels(_ context.Context) ([]providers.Model, error) {
	return nil, nil
}

func TestHandlerFallbackErrorsAreOpenAIShaped(t *testing.T) {
	g := &Gateway{}
	handler := g.newHandler()

	tests := []struct {
		name    string
		method  string
		path    string
		status  int
		errType string
	}{
		{"unknown path", http.MethodPost, "/v1/completions", http.StatusNotFound, providers.ErrorTypeNotFound},
		{"wrong method on chat completions", http.MethodGet, "/v1/chat/completions", http.StatusMethodNotAllowed, providers.ErrorTypeInvalidRequest},
		{"wrong method on models", http.MethodDelete, "/v1/models", http.StatusMethodNotAllowed, providers.ErrorTypeInvalidRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(tt.method, tt.path, nil))
			if rr.Code != tt.status {
				t.Fatalf("%s %s returned %d, want %d", tt.method, tt.path, rr.Code, tt.status)
			}
			var resp providers.ErrorResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("body is not an OpenAI-shaped error: %v; body:\n%s", err, rr.Body.String())
			}
			if resp.Error.Type != tt.errType {
				t.Errorf("error type = %q, want %q", resp.Error.Type, tt.errType)
			}
		})
	}
}

// stubChatProvider returns a canned non-streaming response.
type stubChatProvider struct {
	resp *providers.ChatCompletionResponse
	req  *providers.ChatCompletionRequest
}

func (p *stubChatProvider) ChatCompletion(_ context.Context, req *providers.ChatCompletionRequest) (*providers.ChatCompletionResponse, error) {
	p.req = req
	return p.resp, nil
}

func (p *stubChatProvider) ChatCompletionStream(_ context.Context, _ *providers.ChatCompletionRequest) (<-chan providers.StreamEvent, error) {
	return nil, providers.ErrProviderError("not implemented")
}

func (p *stubChatProvider) ListModels(_ context.Context) ([]providers.Model, error) {
	return nil, nil
}

func TestExplicitModelPassthroughNotRouteRestricted(t *testing.T) {
	rcfg := &routing.RoutingConfig{
		DefaultRoute: "coding",
		Heuristics: &routing.HeuristicsConfig{
			Enabled: true,
			Rules: []routing.HeuristicRule{
				{Match: routing.MatchCondition{Keywords: []string{"translate"}}, Route: "general"},
			},
		},
		Routes: []routing.RouteConfig{
			{Name: "coding", Model: "llama3"},
			{Name: "general", Model: "llama3"},
		},
	}
	sr, err := routing.NewSemanticRouter(context.Background(), rcfg, nil)
	if err != nil {
		t.Fatalf("router construction failed: %v", err)
	}

	stub := &stubChatProvider{resp: &providers.ChatCompletionResponse{ID: "chatcmpl-stub"}}
	pmap := map[providers.ProviderType]providers.Provider{providers.ProviderLocal: stub}
	g := &Gateway{
		cfg:            &Config{},
		providers:      pmap,
		router:         providers.NewRouter(pmap),
		semanticRouter: sr,
		keyStore:       mustKeyStore(t, []keys.EntryConfig{{Name: "restricted", Key: "sk-gw-test", AllowedRoutes: []string{"coding"}}}),
	}
	handler := g.newHandler()

	// an explicit model selects no semantic route; a route-restricted key must
	// not be denied for it.
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"llama3","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer sk-gw-test")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("explicit-model request returned %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	// a semantic decision landing on a disallowed route still denies.
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"translate this please"}]}`))
	req.Header.Set("Authorization", "Bearer sk-gw-test")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("disallowed semantic route returned %d, want 403; body: %s", rr.Code, rr.Body.String())
	}
}

func TestCapabilityModelAliasResolvesBeforeDispatch(t *testing.T) {
	sr, err := routing.NewSemanticRouter(context.Background(), &routing.RoutingConfig{
		Routes: []routing.RouteConfig{{Name: "frontier-coding", Model: "llama3"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	stub := &stubChatProvider{resp: &providers.ChatCompletionResponse{
		ID: "chatcmpl-capability", Model: "upstream-alias",
	}}
	pmap := map[providers.ProviderType]providers.Provider{providers.ProviderLocal: stub}
	g := &Gateway{
		cfg:            &Config{},
		providers:      pmap,
		router:         providers.NewRouter(pmap),
		semanticRouter: sr,
	}
	handler := g.newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"sterling-capability:sterling-classes/v1/frontier-coding","messages":[{"role":"user","content":"hi"}]}`,
	))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("capability request returned %d: %s", rr.Code, rr.Body.String())
	}
	if stub.req == nil || stub.req.Model != "llama3" {
		t.Fatalf("provider request = %+v, want resolved concrete model", stub.req)
	}
	var response providers.ChatCompletionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Model != "llama3" {
		t.Fatalf("reported model = %q, want gateway binding", response.Model)
	}

	allowExplicit := false
	denyExplicitRouter, err := routing.NewSemanticRouter(context.Background(), &routing.RoutingConfig{
		AllowExplicitModel: &allowExplicit,
		Routes:             []routing.RouteConfig{{Name: "frontier-coding", Model: "llama3"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g.semanticRouter = denyExplicitRouter
	stub.req = nil
	handler = g.newHandler()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"sterling-capability:sterling-classes/v1/frontier-coding","messages":[{"role":"user","content":"hi"}]}`,
	))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || stub.req != nil {
		t.Fatalf("explicit-model-disabled capability returned %d, provider request %+v", rr.Code, stub.req)
	}
	g.semanticRouter = sr
	handler = g.newHandler()

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"sterling-capability:sterling-classes/v2/frontier-coding","messages":[{"role":"user","content":"hi"}]}`,
	))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown vocabulary returned %d, want 400", rr.Code)
	}

	g.keyStore = mustKeyStore(t, []keys.EntryConfig{{
		Name: "route-denied", Key: "sk-route-denied", AllowedRoutes: []string{"general"},
	}})
	handler = g.newHandler()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"sterling-capability:sterling-classes/v1/frontier-coding","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer sk-route-denied")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("disallowed capability route returned %d, want 403", rr.Code)
	}

	g.keyStore = mustKeyStore(t, []keys.EntryConfig{{
		Name: "model-denied", Key: "sk-model-denied",
		AllowedRoutes: []string{"frontier-coding"}, AllowedModels: []string{"other-model"},
	}})
	handler = g.newHandler()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"sterling-capability:sterling-classes/v1/frontier-coding","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer sk-model-denied")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("disallowed capability model returned %d, want 403", rr.Code)
	}
}

func TestStreamingChannelCloseWithoutTerminalEvent(t *testing.T) {
	g := &Gateway{}
	provider := &stubStreamProvider{events: []providers.StreamEvent{
		{Chunk: &providers.StreamChunk{ID: "chatcmpl-test", Object: "chat.completion.chunk"}},
	}}

	rr := httptest.NewRecorder()
	g.handleStreamingCompletion(context.Background(), rr, provider, providers.ProviderType(""), "", &providers.ChatCompletionRequest{})

	body := rr.Body.String()
	if strings.Contains(body, "data: [DONE]") {
		t.Errorf("stream without a terminal event must not end with [DONE]; body:\n%s", body)
	}
	if !strings.Contains(body, "upstream stream ended unexpectedly") {
		t.Errorf("expected an SSE error event for the truncated stream; body:\n%s", body)
	}
}

func TestStreamingDoneEmitsDoneSentinel(t *testing.T) {
	g := &Gateway{}
	provider := &stubStreamProvider{events: []providers.StreamEvent{
		{Chunk: &providers.StreamChunk{ID: "chatcmpl-test", Object: "chat.completion.chunk"}},
		{Done: true},
	}}

	rr := httptest.NewRecorder()
	g.handleStreamingCompletion(context.Background(), rr, provider, providers.ProviderType(""), "", &providers.ChatCompletionRequest{})

	body := rr.Body.String()
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("completed stream must end with [DONE]; body:\n%s", body)
	}
	if strings.Contains(body, "upstream stream ended unexpectedly") {
		t.Errorf("completed stream must not carry a truncation error; body:\n%s", body)
	}
}

// TestStreamingUsageCaptureToggle verifies the metrics.stream_usage toggle gates
// whether the gateway requests upstream usage: on -> stream_options.include_usage
// is set on the request; off -> it stays unset.
func TestStreamingUsageCaptureToggle(t *testing.T) {
	events := []providers.StreamEvent{
		{Chunk: &providers.StreamChunk{ID: "chatcmpl-test", Object: "chat.completion.chunk"}},
		{Usage: &providers.Usage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18}},
		{Done: true},
	}

	t.Run("enabled sets include_usage", func(t *testing.T) {
		g := &Gateway{cfg: &Config{Metrics: &MetricsConfig{Enabled: true, StreamUsage: true}}}
		provider := &stubStreamProvider{events: events}
		g.handleStreamingCompletion(context.Background(), httptest.NewRecorder(), provider, providers.ProviderType("openai"), "agent-a", &providers.ChatCompletionRequest{})
		if provider.gotReq.StreamOptions == nil || !provider.gotReq.StreamOptions.IncludeUsage {
			t.Errorf("stream_usage on: want stream_options.include_usage=true, got %+v", provider.gotReq.StreamOptions)
		}
	})

	t.Run("disabled leaves include_usage unset", func(t *testing.T) {
		g := &Gateway{cfg: &Config{Metrics: &MetricsConfig{Enabled: true}}}
		provider := &stubStreamProvider{events: events}
		g.handleStreamingCompletion(context.Background(), httptest.NewRecorder(), provider, providers.ProviderType("openai"), "agent-a", &providers.ChatCompletionRequest{})
		if provider.gotReq.StreamOptions != nil {
			t.Errorf("stream_usage off: want stream_options unset, got %+v", provider.gotReq.StreamOptions)
		}
	})
}
