package gateway

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/michaelquigley/df/dl"
	"github.com/openziti/llm-gateway/providers"
)

type Gateway struct {
	cfg       *Config
	providers map[providers.ProviderType]providers.Provider
	router    *providers.Router
	share     *Share
	access    *Access
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
