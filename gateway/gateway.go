package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/providers"
	"github.com/openziti/llm-gateway/routing"
)

type Gateway struct {
	cfg              *Config
	providers        map[providers.ProviderType]providers.Provider
	router           *providers.Router
	semanticRouter   *routing.SemanticRouter
	share            *Share
	accesses         []*Access
	ollamaHTTPClient *http.Client
	meters           *meters
	metricsHandler   http.Handler
}

func New(cfg *Config) (_ *Gateway, err error) {
	g := &Gateway{
		cfg:       cfg,
		providers: make(map[providers.ProviderType]providers.Provider),
	}
	defer func() {
		if err != nil {
			g.cleanup()
		}
	}()

	if err = g.initProviders(); err != nil {
		return nil, err
	}

	g.router = providers.NewRouter(g.providers)

	if cfg.Metrics != nil && cfg.Metrics.Enabled {
		m, handler, err := initMetrics()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize metrics: %w", err)
		}
		g.meters = m
		g.metricsHandler = handler
		dl.Info("initialized opentelemetry metrics")
	}

	if cfg.Routing != nil {
		sr, err := g.initSemanticRouter(cfg.Routing)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize semantic router: %w", err)
		}
		g.semanticRouter = sr
	}

	return g, nil
}

func (g *Gateway) initProviders() error {
	if g.cfg.Providers == nil {
		return nil
	}

	// initialize openai provider
	if g.cfg.Providers.OpenAI != nil && g.cfg.Providers.OpenAI.APIKey != "" {
		apiKey := os.ExpandEnv(g.cfg.Providers.OpenAI.APIKey)
		baseURL := os.ExpandEnv(g.cfg.Providers.OpenAI.BaseURL)

		if g.cfg.Providers.OpenAI.ZrokShareToken != "" {
			access, err := NewAccess(g.cfg.Providers.OpenAI.ZrokShareToken)
			if err != nil {
				return err
			}
			g.accesses = append(g.accesses, access)
			g.providers[providers.ProviderOpenAI] = providers.NewOpenAIWithClient(apiKey, baseURL, access.HTTPClient())
			dl.Infof("initialized openai provider via zrok share '%s'", g.cfg.Providers.OpenAI.ZrokShareToken)
		} else {
			g.providers[providers.ProviderOpenAI] = providers.NewOpenAI(apiKey, baseURL)
			if baseURL != "" {
				dl.Infof("initialized openai provider at '%s'", baseURL)
			} else {
				dl.Info("initialized openai provider")
			}
		}
	}

	// initialize anthropic provider
	if g.cfg.Providers.Anthropic != nil && g.cfg.Providers.Anthropic.APIKey != "" {
		apiKey := os.ExpandEnv(g.cfg.Providers.Anthropic.APIKey)
		baseURL := os.ExpandEnv(g.cfg.Providers.Anthropic.BaseURL)

		if g.cfg.Providers.Anthropic.ZrokShareToken != "" {
			access, err := NewAccess(g.cfg.Providers.Anthropic.ZrokShareToken)
			if err != nil {
				return err
			}
			g.accesses = append(g.accesses, access)
			g.providers[providers.ProviderAnthropic] = providers.NewAnthropicWithClient(apiKey, baseURL, access.HTTPClient())
			dl.Infof("initialized anthropic provider via zrok share '%s'", g.cfg.Providers.Anthropic.ZrokShareToken)
		} else {
			g.providers[providers.ProviderAnthropic] = providers.NewAnthropic(apiKey, baseURL)
			if baseURL != "" {
				dl.Infof("initialized anthropic provider at '%s'", baseURL)
			} else {
				dl.Info("initialized anthropic provider")
			}
		}
	}

	// initialize ollama provider
	if g.cfg.Providers.Ollama != nil {
		if len(g.cfg.Providers.Ollama.Endpoints) > 0 {
			if err := g.initOllamaMulti(); err != nil {
				return err
			}
		} else {
			g.initOllamaSingle()
		}
	}

	return nil
}

func (g *Gateway) initOllamaSingle() {
	cfg := g.cfg.Providers.Ollama
	if cfg.ZrokShareToken != "" {
		access, err := NewAccess(cfg.ZrokShareToken)
		if err != nil {
			dl.Errorf("failed to create zrok access for ollama: %v", err)
			return
		}
		g.accesses = append(g.accesses, access)
		g.ollamaHTTPClient = access.HTTPClient()
		g.providers[providers.ProviderOllama] = providers.NewOllamaWithClient(cfg.BaseURL, g.ollamaHTTPClient)
		dl.Infof("initialized ollama provider via zrok share '%s'", cfg.ZrokShareToken)
	} else {
		g.providers[providers.ProviderOllama] = providers.NewOllama(cfg.BaseURL)
		dl.Infof("initialized ollama provider at '%s'", cfg.BaseURL)
	}
}

func (g *Gateway) initOllamaMulti() error {
	cfg := g.cfg.Providers.Ollama
	opts := make([]providers.EndpointOption, 0, len(cfg.Endpoints))

	for _, ep := range cfg.Endpoints {
		opt := providers.EndpointOption{
			Name:    ep.Name,
			BaseURL: ep.BaseURL,
		}
		if ep.ZrokShareToken != "" {
			access, err := NewAccess(ep.ZrokShareToken)
			if err != nil {
				return fmt.Errorf("failed to create zrok access for endpoint '%s': %w", ep.Name, err)
			}
			g.accesses = append(g.accesses, access)
			opt.HTTPClient = access.HTTPClient()
		}
		opts = append(opts, opt)
	}

	multi := providers.NewMultiOllama(opts)

	// start health checks
	interval := 30 * time.Second
	timeout := 5 * time.Second
	if cfg.HealthCheck != nil {
		if cfg.HealthCheck.IntervalSeconds > 0 {
			interval = time.Duration(cfg.HealthCheck.IntervalSeconds) * time.Second
		}
		if cfg.HealthCheck.TimeoutSeconds > 0 {
			timeout = time.Duration(cfg.HealthCheck.TimeoutSeconds) * time.Second
		}
	}
	multi.StartHealthChecks(interval, timeout)

	g.providers[providers.ProviderOllama] = multi

	for _, ep := range cfg.Endpoints {
		if ep.ZrokShareToken != "" {
			dl.Infof("initialized ollama endpoint '%s' via zrok share '%s'", ep.Name, ep.ZrokShareToken)
		} else {
			dl.Infof("initialized ollama endpoint '%s' at '%s'", ep.Name, ep.BaseURL)
		}
	}
	dl.Infof("initialized multi-endpoint ollama provider with %d endpoints", len(cfg.Endpoints))

	return nil
}

func (g *Gateway) Run() error {
	handler := g.newHandler()

	// setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer g.cleanup()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		dl.Info("received shutdown signal")
		cancel()
	}()

	if g.cfg.Zrok != nil && g.cfg.Zrok.Share != nil && g.cfg.Zrok.Share.Enabled {
		return g.runWithZrok(ctx, handler)
	}

	return g.runLocal(ctx, handler)
}

func (g *Gateway) runLocal(ctx context.Context, handler http.Handler) error {
	addr := g.cfg.Listen
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	dl.Infof("listening on '%s'", addr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		dl.Info("shutting down server")
		return server.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

func (g *Gateway) runWithZrok(ctx context.Context, handler http.Handler) error {
	var share *Share
	var err error

	if g.cfg.Zrok.Share.Token != "" {
		// use existing persistent share (private only)
		share, err = NewShareFromToken(g.cfg.Zrok.Share.Token)
	} else {
		share, err = NewShare(g.cfg.Zrok.Share.Mode)
	}

	if err != nil {
		return err
	}
	g.share = share

	dl.Infof("serving via zrok share '%s'", share.Token())

	server := &http.Server{
		Handler: handler,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(share.Listener())
	}()

	select {
	case <-ctx.Done():
		dl.Info("shutting down server")
		server.Shutdown(context.Background())
		return nil
	case err := <-errCh:
		return err
	}
}

func (g *Gateway) cleanup() {
	for _, p := range g.providers {
		if c, ok := p.(io.Closer); ok {
			c.Close()
		}
	}
	if g.share != nil {
		g.share.Close()
	}
	for _, access := range g.accesses {
		access.Close()
	}
}

func (g *Gateway) initSemanticRouter(cfg *routing.RoutingConfig) (*routing.SemanticRouter, error) {
	var embedClient routing.Embedder

	// initialize embedding client if semantic matching is enabled
	if cfg.Semantic != nil && cfg.Semantic.Enabled {
		baseURL, apiKey, httpClient := g.resolveEmbedProvider(cfg.Semantic.Provider)
		if baseURL == "" {
			return nil, fmt.Errorf("embedding provider '%s' not configured", cfg.Semantic.Provider)
		}
		if httpClient != nil {
			embedClient = routing.NewEmbedClientWithHTTPClient(cfg.Semantic.Provider, cfg.Semantic.Model, baseURL, apiKey, httpClient)
		} else {
			embedClient = routing.NewEmbedClient(cfg.Semantic.Provider, cfg.Semantic.Model, baseURL, apiKey)
		}
	}

	// resolve classifier provider connection details
	var classifierBaseURL, classifierAPIKey string
	var classifierHTTPClient *http.Client
	if cfg.Classifier != nil && cfg.Classifier.Enabled {
		classifierBaseURL, classifierAPIKey, classifierHTTPClient = g.resolveEmbedProvider(cfg.Classifier.Provider)
		if classifierBaseURL == "" {
			return nil, fmt.Errorf("classifier provider '%s' not configured", cfg.Classifier.Provider)
		}
	}

	ctx := context.Background()
	return routing.NewSemanticRouterWithClassifier(ctx, cfg, embedClient, classifierBaseURL, classifierAPIKey, classifierHTTPClient)
}

// resolveEmbedProvider looks up connection details from provider config.
func (g *Gateway) resolveEmbedProvider(provider string) (baseURL, apiKey string, httpClient *http.Client) {
	if g.cfg.Providers == nil {
		return "", "", nil
	}

	switch provider {
	case "ollama":
		if g.cfg.Providers.Ollama != nil {
			if multi, ok := g.providers[providers.ProviderOllama].(*providers.MultiOllama); ok {
				baseURL = multi.PrimaryBaseURL()
				httpClient = multi.RoundRobinClient()
			} else {
				baseURL = g.cfg.Providers.Ollama.BaseURL
				if baseURL == "" {
					baseURL = "http://localhost:11434"
				}
				httpClient = g.ollamaHTTPClient
			}
		}
	case "openai":
		if g.cfg.Providers.OpenAI != nil {
			apiKey = os.ExpandEnv(g.cfg.Providers.OpenAI.APIKey)
			baseURL = os.ExpandEnv(g.cfg.Providers.OpenAI.BaseURL)
			if baseURL == "" {
				baseURL = "https://api.openai.com"
			}
		}
	}

	return baseURL, apiKey, httpClient
}
