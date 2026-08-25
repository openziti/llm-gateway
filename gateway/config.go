package gateway

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/michaelquigley/df/dd"
	"github.com/openziti/llm-gateway/agora"
	"github.com/openziti/llm-gateway/keys"
	"github.com/openziti/llm-gateway/routing"
)

type Config struct {
	Listen    string
	Zrok      *ZrokConfig
	Agora     *agora.Config
	Providers *ProvidersConfig
	Routing   *routing.RoutingConfig
	Metrics   *MetricsConfig
	APIKeys   *keys.Config
	Tracing   *TracingConfig
}

type TracingConfig struct {
	Enabled          bool
	MaxContentLength int // max characters per message content (default: 200)
}

type ZrokConfig struct {
	Share *ZrokShareConfig
}

type ZrokShareConfig struct {
	Enabled bool
	Mode    string // public or private (default: private)
	Token   string // existing persistent share token (private shares only)
}

type ProvidersConfig struct {
	OpenAI    *OpenAIConfig
	Anthropic *AnthropicConfig
	Local     *LocalConfig
}

type OpenAIConfig struct {
	APIKey         string
	BaseURL        string
	ZrokShareToken string
	AgoraTunnel    string
}

type AnthropicConfig struct {
	APIKey         string
	BaseURL        string
	ZrokShareToken string
	AgoraTunnel    string
}

type LocalConfig struct {
	BaseURL        string
	ZrokShareToken string
	AgoraTunnel    string
	Endpoints      []LocalEndpointConfig
	HealthCheck    *HealthCheckConfig
}

type LocalEndpointConfig struct {
	Name           string
	BaseURL        string
	ZrokShareToken string
	AgoraTunnel    string
	Weight         int
}

type HealthCheckConfig struct {
	IntervalSeconds int
	TimeoutSeconds  int
}

type MetricsConfig struct {
	Enabled bool
	// StreamUsage, when true, captures per-key token usage on the streaming path.
	// It sets stream_options.include_usage on OpenAI/local upstream requests (a
	// behavior change some local backends may not honor) and records the usage
	// Anthropic always streams. Default off for upstream compatibility.
	StreamUsage bool
}

type rawConfigDocument struct {
	Fields map[string]any `dd:",+extra"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := dd.NewYAMLFile[rawConfigDocument](path)
	if err != nil {
		return nil, err
	}

	var keyConfig *keys.Config
	if value, exists := raw.Fields["api_keys"]; exists && value != nil {
		block, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("api_keys: expected mapping")
		}
		keyConfig, err = keys.BindConfig(block)
		if err != nil {
			return nil, err
		}
	}
	// api_keys has already passed its strict subsystem bind. keep the main
	// config's forgiving bind from decoding it a second, weaker way.
	delete(raw.Fields, "api_keys")

	cfg := &Config{}
	if err := dd.Merge(cfg, raw.Fields); err != nil {
		return nil, err
	}
	cfg.APIKeys = keyConfig
	// resolve agora env vars + integration file before any agora field is read.
	if err := agora.ResolveConfig(cfg.Agora); err != nil {
		return nil, err
	}
	if err := keys.ResolveConfig(cfg.APIKeys); err != nil {
		return nil, err
	}
	if err := keys.Validate(cfg.APIKeys); err != nil {
		return nil, err
	}
	if err := cfg.expandEnv(); err != nil {
		return nil, err
	}
	cfg.normalize()
	if err := cfg.validateAgora(); err != nil {
		return nil, err
	}
	if err := cfg.validateProviders(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// expandEnv resolves ${VAR} references in gateway-owned provider fields once at
// load. subsystem-owned fields are resolved by their own ResolveConfig function
// from LoadConfig so downstream gates still see one settled value.
func (c *Config) expandEnv() error {
	expand := func(field string, value *string) error {
		if *value == "" {
			return nil
		}
		expanded := os.ExpandEnv(*value)
		if expanded == "" {
			return fmt.Errorf("%s resolves empty (unset environment variable?)", field)
		}
		*value = expanded
		return nil
	}

	if c.Providers != nil {
		if p := c.Providers.OpenAI; p != nil {
			if err := expand("providers.open_ai.api_key", &p.APIKey); err != nil {
				return err
			}
			if err := expand("providers.open_ai.base_url", &p.BaseURL); err != nil {
				return err
			}
		}
		if p := c.Providers.Anthropic; p != nil {
			if err := expand("providers.anthropic.api_key", &p.APIKey); err != nil {
				return err
			}
			if err := expand("providers.anthropic.base_url", &p.BaseURL); err != nil {
				return err
			}
		}
		if l := c.Providers.Local; l != nil {
			if err := expand("providers.local.base_url", &l.BaseURL); err != nil {
				return err
			}
			for i := range l.Endpoints {
				field := fmt.Sprintf("providers.local.endpoints[%d].base_url", i)
				if err := expand(field, &l.Endpoints[i].BaseURL); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// normalize trims whitespace from agora tunnel names so the provider init
// gates and collectAgoraTunnels read identical values.
func (c *Config) normalize() {
	if c.Providers == nil {
		return
	}
	if p := c.Providers.OpenAI; p != nil {
		p.AgoraTunnel = strings.TrimSpace(p.AgoraTunnel)
	}
	if p := c.Providers.Anthropic; p != nil {
		p.AgoraTunnel = strings.TrimSpace(p.AgoraTunnel)
	}
	if l := c.Providers.Local; l != nil {
		l.AgoraTunnel = strings.TrimSpace(l.AgoraTunnel)
		for i := range l.Endpoints {
			l.Endpoints[i].AgoraTunnel = strings.TrimSpace(l.Endpoints[i].AgoraTunnel)
		}
	}
}

// validateProviders enforces that an explicitly configured overlay transport on
// a cloud provider can actually be honored: the provider only initializes with
// an API key, so an overlay without one would silently evaporate. runs after
// expandEnv, so the key values here are already resolved.
func (c *Config) validateProviders() error {
	if c.Providers == nil {
		return nil
	}
	check := func(name, apiKey, zrokToken, agoraTunnel string) error {
		if apiKey != "" {
			return nil
		}
		if agoraTunnel != "" {
			return fmt.Errorf("providers.%s.agora_tunnel is set but providers.%s.api_key is empty", name, name)
		}
		if zrokToken != "" {
			return fmt.Errorf("providers.%s.zrok_share_token is set but providers.%s.api_key is empty", name, name)
		}
		return nil
	}
	if p := c.Providers.OpenAI; p != nil {
		if err := check("open_ai", p.APIKey, p.ZrokShareToken, p.AgoraTunnel); err != nil {
			return err
		}
	}
	if p := c.Providers.Anthropic; p != nil {
		if err := check("anthropic", p.APIKey, p.ZrokShareToken, p.AgoraTunnel); err != nil {
			return err
		}
	}
	// multi-endpoint mode reads only per-endpoint transports; a top-level
	// overlay on the local block would be silently ignored.
	if l := c.Providers.Local; l != nil && len(l.Endpoints) > 0 {
		if l.AgoraTunnel != "" {
			return fmt.Errorf("providers.local.agora_tunnel is ignored in multi-endpoint mode; move it onto an endpoint")
		}
		if l.ZrokShareToken != "" {
			return fmt.Errorf("providers.local.zrok_share_token is ignored in multi-endpoint mode; move it onto an endpoint")
		}
	}

	// a written base_url the gateway cannot dial is configuration it cannot
	// honor. deferring it to the request path starts the gateway reporting
	// healthy while every affected call fails.
	if p := c.Providers.OpenAI; p != nil {
		if err := validateBaseURL("providers.open_ai.base_url", p.BaseURL); err != nil {
			return err
		}
	}
	if p := c.Providers.Anthropic; p != nil {
		if err := validateBaseURL("providers.anthropic.base_url", p.BaseURL); err != nil {
			return err
		}
	}
	if l := c.Providers.Local; l != nil {
		if err := validateBaseURL("providers.local.base_url", l.BaseURL); err != nil {
			return err
		}
		for i := range l.Endpoints {
			field := fmt.Sprintf("providers.local.endpoints[%d].base_url", i)
			if err := validateBaseURL(field, l.Endpoints[i].BaseURL); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateBaseURL accepts an empty value as "not configured" and otherwise
// requires an absolute HTTP(S) URL with a host, matching what the key
// subsystem requires of a source base_url.
func validateBaseURL(field, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" ||
		(!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", field)
	}
	return nil
}

// AgoraServeEnabled reports whether the gateway should serve its handler over
// an agora tunnel.
func (c *Config) AgoraServeEnabled() bool {
	return c != nil && c.Agora != nil && c.Agora.Enabled && agora.ServeEnabled(c.Agora)
}

// AgoraPublishEnabled reports whether the gateway should publish a catalog
// advertisement. publishing requires serving in this iteration: a dial-only
// gateway never publishes an advertisement whose name points at a front-door
// tunnel it does not bind.
func (c *Config) AgoraPublishEnabled() bool {
	return c.AgoraServeEnabled() && agora.AdvertisementPublish(c.Agora)
}

// collectAgoraTunnels returns the unique, trimmed agora_tunnel names for only
// the providers, endpoints, and key sources their init paths will construct —
// no phantom attachments.
func collectAgoraTunnels(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var tunnels []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		tunnels = append(tunnels, name)
	}

	if cfg.Providers != nil {
		if p := cfg.Providers.OpenAI; p != nil && p.APIKey != "" {
			add(p.AgoraTunnel)
		}
		if p := cfg.Providers.Anthropic; p != nil && p.APIKey != "" {
			add(p.AgoraTunnel)
		}
		if l := cfg.Providers.Local; l != nil {
			if len(l.Endpoints) > 0 {
				for _, ep := range l.Endpoints {
					add(ep.AgoraTunnel)
				}
			} else {
				add(l.AgoraTunnel)
			}
		}
	}
	if cfg.APIKeys != nil && cfg.APIKeys.Enabled {
		for _, dynamic := range cfg.APIKeys.Sources {
			if source, ok := dynamic.(*keys.HTTPSourceConfig); ok {
				add(source.AgoraTunnel)
			}
		}
	}
	return tunnels
}

// validateAgora enforces the fail-fast preconditions that keep per-site
// agora_tunnel values and agora.serve.enabled meaningful: each requires the
// agora subsystem, and an explicit publish request requires serving.
func (c *Config) validateAgora() error {
	// (a) dial side — a per-site agora_tunnel is meaningless without the subsystem.
	if len(collectAgoraTunnels(c)) > 0 && (c.Agora == nil || !c.Agora.Enabled) {
		return fmt.Errorf("agora_tunnel set on a provider, endpoint, or key source requires agora.enabled: true")
	}
	// (b) serve side (symmetric) — serve.enabled without enabled would silently
	// fall back to the plaintext local listener.
	if c.Agora != nil && c.Agora.Serve != nil && c.Agora.Serve.Enabled && !c.Agora.Enabled {
		return fmt.Errorf("agora.serve.enabled requires agora.enabled: true")
	}
	// (c) explicit publish: true without serve — honor the request loudly, not
	// silently. an explicit false is an opt-out and needs no serve.
	if agora.PublishExplicit(c.Agora) && agora.AdvertisementPublish(c.Agora) && !c.AgoraServeEnabled() {
		return fmt.Errorf("agora.advertisement.publish requires agora.serve.enabled in this iteration")
	}
	return nil
}
