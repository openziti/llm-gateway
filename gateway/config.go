package gateway

import (
	"github.com/michaelquigley/df/dd"
	"github.com/openziti/llm-gateway/routing"
)

type Config struct {
	Listen    string
	Zrok      *ZrokConfig
	Providers *ProvidersConfig
	Routing   *routing.RoutingConfig
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
	Ollama    *OllamaConfig
}

type OpenAIConfig struct {
	APIKey         string
	BaseURL        string
	ZrokShareToken string
}

type AnthropicConfig struct {
	APIKey         string
	BaseURL        string
	ZrokShareToken string
}

type OllamaConfig struct {
	BaseURL        string
	ZrokShareToken string
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}
	if err := dd.MergeYAMLFile(cfg, path); err != nil {
		return nil, err
	}
	return cfg, nil
}
