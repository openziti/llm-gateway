# llm-gateway

An OpenAI-compatible API proxy that routes requests to OpenAI, Anthropic, and any OpenAI-compatible backend (Ollama, vLLM, llama-server, SGLang, etc.). Optionally expose the gateway via [zrok](https://zrok.io) for zero-trust access.

## Why another LLM gateway?

Most LLM proxies solve API translation. This one also solves the network problem: how do you connect a gateway to GPU boxes behind NAT, expose it to clients without opening ports, and route requests to the right model — all without bolting on a VPN, a service mesh, or a routing database?

- **Zero-trust networking with zrok (over OpenZiti)** — the gateway and its backends communicate using [zrok](https://github.com/openziti/zrok) over [OpenZiti](https://openziti.io) overlay networks. Expose the gateway or reach a backend across NAT, air-gapped networks, or cloud boundaries without firewall rules or port forwarding. Both directions work the same way.
- **Semantic routing** — a three-layer cascade (keyword heuristics, embedding similarity, LLM classifier) selects the best model automatically when clients omit the `model` field. No hand-maintained routing tables.
- **Multi-endpoint load balancing** — weighted round-robin, health checks with passive failover, and VM sleep detection across a pool of inference servers. Works with Ollama, llama-server, vLLM, SGLang, or anything that exposes `/v1/chat/completions`. Built for distributing inference across real hardware.
- **Single binary, zero infrastructure** — one Go binary, one YAML file. No database, no message queue, no sidecar.

## Features

- **OpenAI-compatible API**: Drop-in replacement for OpenAI client libraries
- **Multi-provider routing**: Automatically routes requests based on model name
- **Semantic routing**: Optional three-layer cascade (heuristics, embeddings, LLM classifier) to automatically select the best model when `model` is omitted
- **Anthropic translation**: Transparently converts OpenAI format to/from Anthropic's Messages API
- **Streaming support**: Server-Sent Events (SSE) streaming for all providers
- **Multi-endpoint load balancing**: Round-robin load distribution and automatic failover across multiple inference backends
- **OpenTelemetry metrics**: Prometheus-exported metrics for requests, latency, tokens, and endpoint health
- **zrok integration**: Expose the gateway via zrok private or public shares
- **Zero-trust backends**: Connect to any provider via zrok shares (no exposed ports)

> **New here?** See the [Getting Started guide](docs/getting-started.md) for a step-by-step walkthrough from zero to a working gateway.

## Installation

Pre-built binaries for Linux, macOS, and Windows are available on the [Releases](https://github.com/openziti/llm-gateway/releases) page.

Or install with Go:

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
  local:                               # works with any OpenAI-compatible backend
    base_url: "http://localhost:11434"
```

2. Run the gateway:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
llm-gateway run config.yaml
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
| Everything else | Local / self-hosted |

Any model that doesn't match the OpenAI or Anthropic prefixes is routed to the local provider. It works with any OpenAI-compatible backend — Ollama, vLLM, llama-server, SGLang, or similar.

### Multi-Endpoint Load Balancing

Distribute requests across multiple inference backends for load balancing and resilience. When `endpoints` is present, the gateway uses round-robin selection with automatic failover:

```yaml
providers:
  local:
    endpoints:
      - name: gpu-box-1
        base_url: "http://10.0.0.1:11434"
        weight: 3
      - name: gpu-box-2
        base_url: "http://10.0.0.2:11434"
      - name: remote
        zrok_share_token: "abc123"
    health_check:
      interval_seconds: 30   # default: 30
      timeout_seconds: 5     # default: 5
```

Each endpoint can use direct HTTP or a zrok share. The optional `weight` (default: 1) controls the proportion of traffic an endpoint receives — an endpoint with weight 3 gets ~3x the requests of weight 1. A background goroutine pings each endpoint at the configured interval and marks unhealthy endpoints for automatic skip. Network errors during requests also trigger immediate passive failover. All gateway features that use the local provider (chat completions, embeddings, classifier) distribute requests across the endpoint group.

The endpoints don't all have to run the same software. You can mix Ollama, vLLM, llama-server, or any other OpenAI-compatible server in the same pool — the load balancing layer doesn't care what's behind the URL.

## API Endpoints

### POST /v1/chat/completions

OpenAI-compatible chat completions endpoint. Supports both streaming (`stream: true`) and non-streaming requests. When semantic routing is enabled, the `model` field is optional.

### GET /v1/models

Returns available models from all configured providers. When semantic routing is enabled, includes an `auto` virtual model that triggers automatic model selection. With multi-endpoint mode, returns the deduplicated union of models from all healthy endpoints.

### GET /health

Returns `{"status":"ok"}` with HTTP 200. Use for liveness checks.

### GET /metrics

Prometheus metrics endpoint (when `metrics.enabled: true`). Exposes request counts, duration histograms, token counters, routing decisions, provider errors, in-flight gauges, and endpoint health.

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

  local:
    base_url: "http://localhost:11434"
    zrok_share_token: ""     # optional: connect via zrok share

    # or use multi-endpoint mode for round-robin + failover:
    # endpoints:
    #   - name: gpu-box-1
    #     base_url: "http://10.0.0.1:11434"
    #   - name: gpu-box-2
    #     base_url: "http://10.0.0.2:11434"
    #   - name: remote
    #     zrok_share_token: "abc123"
    # health_check:
    #   interval_seconds: 30
    #   timeout_seconds: 5

metrics:
  enabled: false
```

### Environment Variables

API keys support environment variable expansion using `${VAR}` syntax.

## API Keys

The gateway supports virtual API keys for client authentication. Generate a key and add it to the config:

```bash
llm-gateway genkey
# sk-gw-a1b2c3d4e5f6...
```

```yaml
api_keys:
  enabled: true
  keys:
    - name: alice
      key: "sk-gw-a1b2c3d4e5f6..."
      allowed_models: ["*"]
```

Clients send the key via the `Authorization: Bearer <key>` header. The `/health` and `/metrics` endpoints remain unauthenticated. Keys can be restricted to specific models using glob patterns. See [docs/api-keys.md](docs/api-keys.md) for details.

## Semantic Routing

When configured, the gateway can automatically select the best model for a request based on its content. The `model` field becomes optional — if omitted, the request passes through a three-layer cascade:

1. **Heuristics** — fast keyword/pattern matching (e.g. "translate" -> fast model, tool use -> tool-capable model)
2. **Embeddings** — cosine similarity between the user prompt and route exemplars using Ollama or OpenAI embeddings
3. **LLM Classifier** — asks an LLM to classify the request when embeddings are ambiguous

Each layer can be independently enabled or disabled — for example, the classifier can be used without embeddings. If all layers are skipped or inconclusive, the configured default route is used. When semantic routing is disabled or unconfigured, behavior is unchanged.

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
      - match:
          system_prompt_contains: "you are a code assistant"
        route: coding
      - match:
          max_tokens_lt: 100
          message_length_lt: 200
        route: fast

  semantic:
    enabled: true
    provider: local             # local or openai
    model: nomic-embed-text
    threshold: 0.82             # confident match
    ambiguous_threshold: 0.65   # escalate to classifier
    comparison: centroid         # centroid, max, or average

  classifier:
    enabled: true
    provider: local
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

    - name: creative
      model: claude-sonnet-4-20250514
      description: "creative writing, storytelling, and artistic content"
      examples:
        - "write a poem about the ocean"
        - "tell me a story about a dragon"

    - name: fast
      model: llama3
      description: "simple tasks, translations, and short responses"
      examples:
        - "translate hello to French"
        - "what is 2+2"

    - name: tools
      model: gpt-4
      description: "tasks requiring tool use and function calling"
      examples:
        - "search the web for recent news"
        - "call the weather API for New York"

    - name: general
      model: llama3
      description: "general conversation and miscellaneous tasks"
      examples:
        - "what is the capital of France"
        - "explain quantum computing"
```

### Heuristic Match Conditions

Each heuristic rule has a `match` block with one or more conditions. When multiple conditions are specified in a single rule, all must match (AND logic). Within `keywords`, any keyword matching triggers the rule (OR logic). Available conditions:

| Condition | Description |
|-----------|-------------|
| `keywords` | Case-insensitive word-boundary match against user message content |
| `has_tools` | Matches if request includes/lacks tool definitions |
| `system_prompt_contains` | Substring match on the system message |
| `max_tokens_lt` | Matches if `max_tokens` is below a threshold |
| `message_length_lt` | Matches if total message character length is below a threshold |
| `exclude` | List of phrases that suppress keyword matches if found in user messages |

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

## Metrics

Enable OpenTelemetry metrics with a Prometheus exporter:

```yaml
metrics:
  enabled: true
```

When enabled, the gateway serves Prometheus metrics at `GET /metrics` on the same listen address. Available metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `llm_gateway.requests` | Counter | Total requests (by provider, model, streaming) |
| `llm_gateway.request.duration` | Histogram | Request duration in seconds (by provider, model) |
| `llm_gateway.tokens.prompt` | Counter | Total prompt tokens (by provider, model) |
| `llm_gateway.tokens.completion` | Counter | Total completion tokens (by provider, model) |
| `llm_gateway.routing.decisions` | Counter | Semantic routing decisions (by method) |
| `llm_gateway.provider.errors` | Counter | Provider errors (by error_type) |
| `llm_gateway.requests.inflight` | Gauge | Currently in-flight requests |
| `llm_gateway.endpoint.healthy` | Gauge | Endpoint health status |

## Tracing

Enable request body logging for debugging routing decisions:

```yaml
tracing:
  enabled: true
  max_content_length: 200   # max characters per message in log output
```

Each chat completion request is logged with the model, message count, streaming flag, tool count, and each message's role and truncated content. See [docs/configuration.md](docs/configuration.md) for details.

## CLI Reference

```
llm-gateway run <configPath>

Flags:
      --address string     listen address (overrides config)
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
llm-gateway run config.yaml --zrok --zrok-mode private
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

  local:
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

# Routes to local backend (Ollama, vLLM, etc.)
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

## Documentation

- [Getting Started](docs/getting-started.md) -- step-by-step setup guide
- [Configuration](docs/configuration.md) -- full config reference and CLI flags
- [Providers](docs/providers.md) -- provider details, streaming, and error handling
- [Semantic Routing](docs/semantic-routing.md) -- threshold tuning, comparison modes, and caching
- [API Keys](docs/api-keys.md) -- per-key model and route restrictions
- [Multi-Endpoint Load Balancing](docs/multi-endpoint.md) -- weighted load balancing and failover
- [Metrics](docs/metrics.md) -- Prometheus instruments and example queries
- [Streaming](docs/streaming.md) -- SSE streaming details
- [zrok](docs/zrok.md) -- overlay networking for sharing and access

## License

Apache 2.0
