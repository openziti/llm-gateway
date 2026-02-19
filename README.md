# llm-gateway

An OpenAI-compatible API proxy that routes requests to OpenAI, Anthropic, or Ollama backends. Optionally expose the gateway via [zrok](https://zrok.io) for zero-trust access.

## Features

- **OpenAI-compatible API**: Drop-in replacement for OpenAI client libraries
- **Multi-provider routing**: Automatically routes requests based on model name
- **Semantic routing**: Optional three-layer cascade (heuristics, embeddings, LLM classifier) to automatically select the best model when `model` is omitted
- **Anthropic translation**: Transparently converts OpenAI format to/from Anthropic's Messages API
- **Streaming support**: Server-Sent Events (SSE) streaming for all providers
- **zrok integration**: Expose the gateway via zrok private or public shares
- **Zero-trust backends**: Connect to any provider via zrok shares (no exposed ports)

## Installation

```bash
go install github.com/openziti/llm-gateway/cmd/llm-gateway@latest
```

Or build from source:

```bash
git clone https://github.com/openziti/llm-gateway.git
cd llm-gateway
go install ./...
```

## Quick Start

1. Create a config file:

```yaml
# config.yaml
listen: ":8080"

providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
  ollama:
    base_url: "http://localhost:11434"
```

2. Run the gateway:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
llm-gateway run --config config.yaml
```

3. Make requests using any OpenAI-compatible client:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

## Provider Routing

Requests are routed based on model name prefix:

| Model Prefix | Provider |
|--------------|----------|
| `gpt-*` | OpenAI |
| `o1-*` | OpenAI |
| `o3-*` | OpenAI |
| `claude-*` | Anthropic |
| Everything else | Ollama |

## API Endpoints

### POST /v1/chat/completions

OpenAI-compatible chat completions endpoint. Supports both streaming (`stream: true`) and non-streaming requests. When semantic routing is enabled, the `model` field is optional.

### GET /v1/models

Returns available models from all configured providers. When semantic routing is enabled, includes an `auto` virtual model that triggers automatic model selection.

## Configuration

### Full Configuration Example

```yaml
listen: ":8080"

zrok:
  share:
    enabled: false
    mode: private      # public or private
    token: ""          # use existing persistent share (private only)

providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"
    base_url: ""             # optional: override for Azure or compatible APIs
    zrok_share_token: ""     # optional: connect via zrok share

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    base_url: ""             # optional: override base URL
    zrok_share_token: ""     # optional: connect via zrok share

  ollama:
    base_url: "http://localhost:11434"
    zrok_share_token: ""     # optional: connect via zrok share
```

### Environment Variables

API keys support environment variable expansion using `${VAR}` syntax.

## Semantic Routing

When configured, the gateway can automatically select the best model for a request based on its content. The `model` field becomes optional — if omitted, the request passes through a three-layer cascade:

1. **Heuristics** — fast keyword/pattern matching (e.g. "translate" -> fast model, tool use -> tool-capable model)
2. **Embeddings** — cosine similarity between the user prompt and route exemplars using Ollama or OpenAI embeddings
3. **LLM Classifier** — asks an LLM to classify the request when embeddings are ambiguous

Each layer can be independently enabled or disabled. If all layers are skipped or inconclusive, the configured default route is used. When semantic routing is disabled or unconfigured, behavior is unchanged.

### Semantic Routing Configuration

```yaml
routing:
  allow_explicit_model: true    # clients can still specify a model directly
  default_route: general        # fallback when no layer matches

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: ["translate", "translation"]
        route: fast
      - match:
          has_tools: true
        route: tools

  semantic:
    enabled: true
    provider: ollama            # ollama or openai
    model: nomic-embed-text
    threshold: 0.82             # confident match
    ambiguous_threshold: 0.65   # escalate to classifier
    comparison: centroid         # centroid, max, or average

  classifier:
    enabled: true
    provider: ollama
    model: llama3
    timeout_ms: 5000
    confidence_threshold: 0.7

  routes:
    - name: coding
      model: claude-sonnet-4-20250514
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
        - "debug this segfault in my C code"

    - name: fast
      model: llama3
      description: "simple tasks, translations, and short responses"
      examples:
        - "translate hello to French"
        - "what is 2+2"

    - name: general
      model: llama3
      description: "general conversation and miscellaneous tasks"
      examples:
        - "what is the capital of France"
        - "explain quantum computing"
```

### Sending Requests Without a Model

With semantic routing enabled, the `model` field can be omitted:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Write a Python function to sort a list"}]
  }'
```

### Using with Chat Clients (Open WebUI, etc.)

Chat clients like Open WebUI require selecting a model from the model list — they always send a `model` field. When semantic routing is enabled, the gateway exposes a virtual `auto` model that triggers automatic routing:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "auto",
    "messages": [{"role": "user", "content": "Write a Python function to sort a list"}]
  }'
```

Point your client at `http://localhost:8080/v1` and select `auto` from the model list. The gateway will route each request through the semantic routing cascade.

The gateway logs each routing decision with the method used, confidence score, latency, and cascade trace.

## CLI Reference

```
llm-gateway run [flags]

Flags:
      --address string     listen address (overrides config)
  -c, --config string      path to config file (default "etc/config.yaml")
      --zrok               enable zrok sharing (overrides config)
      --zrok-mode string   zrok share mode: public, private (overrides config)
```

## zrok Integration

### Exposing the Gateway

Enable zrok sharing to expose the gateway without opening firewall ports:

```yaml
zrok:
  share:
    enabled: true
    mode: private
```

Or via CLI:

```bash
llm-gateway run --zrok --zrok-mode private
```

The gateway will log the share token on startup. Clients connect using zrok access.

### Using Persistent Shares

For stable share tokens across restarts, create a persistent share with the zrok CLI and reference it:

```yaml
zrok:
  share:
    enabled: true
    token: "abc123xyz"  # your persistent share token
```

### Connecting to Backends via zrok

Any provider can be reached through a zrok share instead of a direct URL:

```yaml
providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"
    zrok_share_token: "openai-proxy-share-token"

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    zrok_share_token: "anthropic-proxy-share-token"

  ollama:
    zrok_share_token: "ollama-share-token"
```

## Examples

### Using with Python OpenAI Client

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="not-needed"  # gateway handles auth
)

# Routes to OpenAI
response = client.chat.completions.create(
    model="gpt-4o",
    messages=[{"role": "user", "content": "Hello!"}]
)

# Routes to Anthropic (translated automatically)
response = client.chat.completions.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "Hello!"}]
)

# Routes to Ollama
response = client.chat.completions.create(
    model="llama3.2",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

### Streaming

```python
stream = client.chat.completions.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "Write a haiku"}],
    stream=True
)

for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

## License

Apache 2.0
