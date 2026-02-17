package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/providers"
	"github.com/openziti/llm-gateway/routing"
)

type Gateway struct {
	cfg            *Config
	providers      map[providers.ProviderType]providers.Provider
	router         *providers.Router
	semanticRouter *routing.SemanticRouter
	share          *Share
	access         *Access
}

func New(cfg *Config) (*Gateway, error) {
	g := &Gateway{
		cfg:       cfg,
		providers: make(map[providers.ProviderType]providers.Provider),
	}

	if err := g.initProviders(); err != nil {
		return nil, err
	}

	g.router = providers.NewRouter(g.providers)

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
		g.providers[providers.ProviderOpenAI] = providers.NewOpenAI(apiKey, baseURL)
		if baseURL != "" {
			dl.Infof("initialized openai provider at '%s'", baseURL)
		} else {
			dl.Info("initialized openai provider")
		}
	}

	// initialize anthropic provider
	if g.cfg.Providers.Anthropic != nil && g.cfg.Providers.Anthropic.APIKey != "" {
		apiKey := os.ExpandEnv(g.cfg.Providers.Anthropic.APIKey)
		baseURL := os.ExpandEnv(g.cfg.Providers.Anthropic.BaseURL)
		g.providers[providers.ProviderAnthropic] = providers.NewAnthropic(apiKey, baseURL)
		if baseURL != "" {
			dl.Infof("initialized anthropic provider at '%s'", baseURL)
		} else {
			dl.Info("initialized anthropic provider")
		}
	}

	// initialize ollama provider
	if g.cfg.Providers.Ollama != nil {
		var ollama *providers.Ollama

		if g.cfg.Providers.Ollama.ZrokShare != "" {
			// connect to ollama via zrok
			access, err := NewAccess(g.cfg.Providers.Ollama.ZrokShare)
			if err != nil {
				return err
			}
			g.access = access
			ollama = providers.NewOllamaWithClient(g.cfg.Providers.Ollama.BaseURL, access.HTTPClient())
			dl.Infof("initialized ollama provider via zrok share '%s'", g.cfg.Providers.Ollama.ZrokShare)
		} else {
			ollama = providers.NewOllama(g.cfg.Providers.Ollama.BaseURL)
			dl.Infof("initialized ollama provider at '%s'", g.cfg.Providers.Ollama.BaseURL)
		}

		g.providers[providers.ProviderOllama] = ollama
	}

	return nil
}

func (g *Gateway) Run() error {
	handler := g.newHandler()

	// setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		dl.Info("received shutdown signal")
		cancel()
		g.cleanup()
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
	if g.share != nil {
		g.share.Close()
	}
	if g.access != nil {
		g.access.Close()
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
	if cfg.Classifier != nil && cfg.Classifier.Enabled {
		classifierBaseURL, classifierAPIKey, _ = g.resolveEmbedProvider(cfg.Classifier.Provider)
		if classifierBaseURL == "" {
			return nil, fmt.Errorf("classifier provider '%s' not configured", cfg.Classifier.Provider)
		}
	}

	ctx := context.Background()
	return routing.NewSemanticRouterWithClassifier(ctx, cfg, embedClient, classifierBaseURL, classifierAPIKey)
}

// resolveEmbedProvider looks up connection details from provider config.
func (g *Gateway) resolveEmbedProvider(provider string) (baseURL, apiKey string, httpClient *http.Client) {
	if g.cfg.Providers == nil {
		return "", "", nil
	}

	switch provider {
	case "ollama":
		if g.cfg.Providers.Ollama != nil {
			baseURL = g.cfg.Providers.Ollama.BaseURL
			if g.access != nil {
				httpClient = g.access.HTTPClient()
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
