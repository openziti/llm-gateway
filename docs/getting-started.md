# Getting Started

This guide walks you through setting up llm-gateway, starting with a minimal single-provider config and expanding into a production-ready deployment.

## Prerequisites

- **Go** (1.25+) to build from source
- At least one backend:
  - [Ollama](https://ollama.com) running locally, or
  - An OpenAI or Anthropic API key

## 1. Install

```bash
go install github.com/openziti/llm-gateway/cmd/llm-gateway@latest
```

Or clone and build locally:

```bash
git clone https://github.com/openziti/llm-gateway.git
cd llm-gateway
go install ./...
```

## 2. Minimal Config: Local Ollama

The simplest setup proxies a local Ollama instance. Create `config.yaml`:

```yaml
listen: ":8080"

providers:
  ollama:
    base_url: "http://localhost:11434"
```

Start the gateway:

```bash
llm-gateway run config.yaml
```

Verify it's running:

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

List available models (these come from your Ollama instance):

```bash
curl http://localhost:8080/v1/models
```

Make a test request:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

At this point you have a working OpenAI-compatible API backed by Ollama. Any tool that speaks the OpenAI API (Open WebUI, Continue, etc.) can point at `http://localhost:8080` and use your local models.

## 3. Adding Cloud Providers

Extend the config to add OpenAI and/or Anthropic:

```yaml
listen: ":8080"

providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"

  ollama:
    base_url: "http://localhost:11434"
```

API keys are expanded from environment variables at startup. Export them before running:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
llm-gateway run config.yaml
```

The gateway routes requests by model name prefix:

| Prefix | Provider |
|---|---|
| `gpt-*`, `o1-*`, `o3-*` | OpenAI |
| `claude-*` | Anthropic |
| everything else | Ollama |

All three providers speak the same OpenAI-compatible format -- the Anthropic translation is handled transparently. A client can switch between `gpt-4`, `claude-sonnet-4-20250514`, and `llama3` just by changing the `model` field.

```bash
# hits OpenAI
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello!"}]}'

# hits Anthropic
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-sonnet-4-20250514", "messages": [{"role": "user", "content": "Hello!"}]}'
```

Only configured providers are available. If you don't need OpenAI, leave its block out entirely.

See [providers.md](providers.md) for details on request/response translation and error handling.

## 4. Securing the Gateway with API Keys

By default the gateway is open -- any client can make requests. To require authentication, generate a key and add it to the config:

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

Clients must now send the key in the `Authorization` header:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-gw-a1b2c3d4e5f6..." \
  -H "Content-Type: application/json" \
  -d '{"model": "llama3", "messages": [{"role": "user", "content": "Hello!"}]}'
```

The `/health` and `/metrics` endpoints remain unauthenticated.

You can restrict keys to specific models using glob patterns:

```yaml
keys:
  - name: alice
    key: "sk-gw-..."
    allowed_models: ["claude-*", "gpt-*"]   # cloud models only

  - name: ci-pipeline
    key: "${CI_GATEWAY_KEY}"
    allowed_models: ["llama3"]              # local models only
```

See [api-keys.md](api-keys.md) for route restrictions and the full authentication flow.

## 5. Semantic Routing

Semantic routing lets the gateway choose the best model automatically when a client omits the `model` field (or sends `model: auto`). This is useful when you want different models for different types of requests -- e.g., a coding model for programming questions and a fast local model for general chat.

### Define Routes

Each route maps a name to a backend model:

```yaml
routing:
  default_route: general

  routes:
    - name: coding
      model: claude-sonnet-4-20250514
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
        - "debug this segfault in my C code"
        - "review this pull request for bugs"

    - name: general
      model: llama3
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"
        - "explain how photosynthesis works"
        - "translate hello to Japanese"
```

### Add Heuristic Rules

Heuristics are fast keyword-based rules evaluated before any model calls:

```yaml
routing:
  default_route: general

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: ["code", "debug", "refactor"]
          exclude: ["code fences", "code block"]
        route: coding
      - match:
          has_tools: true
        route: coding

  routes:
    # ... same as above
```

Rules are evaluated in order -- the first match wins. The `exclude` field prevents false positives from boilerplate text (e.g., Open WebUI meta-prompts that mention "code fences").

### Add Embedding-Based Routing

For requests that don't match any heuristic, embedding similarity compares the user's message against the route examples. This requires an embedding model in Ollama:

```bash
ollama pull nomic-embed-text
```

```yaml
routing:
  default_route: general

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: ["code", "debug", "refactor"]
          exclude: ["code fences", "code block"]
        route: coding

  semantic:
    enabled: true
    provider: ollama
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5

  routes:
    # ... same as above
```

### Add the LLM Classifier

For ambiguous cases where embedding similarity falls between the two thresholds, an LLM classifier can act as a tiebreaker:

```yaml
  classifier:
    enabled: true
    provider: ollama
    model: llama3
    timeout_ms: 5000
    confidence_threshold: 0.7
```

The full cascade is: heuristics -> embeddings -> classifier -> default route. Each layer only runs if the previous one didn't produce a confident result.

Test it by sending a request without a model:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"messages": [{"role": "user", "content": "write a binary search in Go"}]}'
```

The gateway logs show which layer made the decision and the confidence scores.

See [semantic-routing.md](semantic-routing.md) for threshold tuning, comparison modes, caching, and the full config reference.

## 6. Scaling Ollama with Multi-Endpoint

When you have multiple Ollama instances, the gateway can distribute requests across them with weighted round-robin and automatic failover. Replace `base_url` with an `endpoints` list:

```yaml
providers:
  ollama:
    endpoints:
      - name: local-gpu
        base_url: "http://localhost:11434"
      - name: remote-gpu
        base_url: "http://10.0.0.2:11434"
        weight: 2
    health_check:
      interval_seconds: 30
      timeout_seconds: 5
```

The `weight` controls traffic proportion -- the remote GPU above gets ~2x the requests of the local one. Health checks run in the background; if an endpoint goes down, traffic automatically shifts to the remaining healthy endpoints.

Embedding and classifier requests (when using `provider: ollama` for semantic routing) use the same round-robin distribution.

See [ollama-multi-endpoint.md](ollama-multi-endpoint.md) for failover behavior and more examples.

## 7. Sharing over Zrok

[zrok](https://zrok.io) lets you expose the gateway -- or reach remote backends -- over an overlay network without opening ports or configuring DNS.

### Exposing the Gateway

Share the gateway so remote clients can reach it:

```yaml
zrok:
  share:
    enabled: true
    mode: private
```

Or via CLI flags:

```bash
llm-gateway run config.yaml --zrok --zrok-mode private
```

The share token is logged at startup. Clients connect using the token rather than an IP address.

### Reaching a Remote Ollama

If Ollama runs on a different machine with a zrok share, connect to it without direct network access:

```yaml
providers:
  ollama:
    zrok_share_token: "remote-ollama-token"
```

This works for any provider and can be mixed with direct HTTP in multi-endpoint configs.

The host machine must have a zrok environment enabled (`zrok enable`). See [zrok.md](zrok.md) for persistent shares and more details.

## 8. Metrics and Tracing

### Prometheus Metrics

Enable metrics to track request counts, latency, token usage, routing decisions, and provider errors:

```yaml
metrics:
  enabled: true
```

Scrape `GET /metrics` with Prometheus:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: llm-gateway
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:8080"]
```

See [metrics.md](metrics.md) for the full list of instruments and example PromQL queries.

### Request Tracing

Enable tracing to log a structured summary of each request, including message roles and truncated content. This is especially useful for debugging semantic routing decisions:

```yaml
tracing:
  enabled: true
  max_content_length: 200
```

Each request is logged with the model, message count, streaming flag, and each message's role and content. Keep this off in production unless you're actively debugging.

## Putting It All Together

A full config combining all of the above:

```yaml
listen: ":8080"

providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"

  ollama:
    endpoints:
      - name: local
        base_url: "http://localhost:11434"
      - name: remote
        zrok_share_token: "gpu-box-token"
        weight: 2
    health_check:
      interval_seconds: 30
      timeout_seconds: 5

api_keys:
  enabled: true
  keys:
    - name: alice
      key: "${ALICE_GATEWAY_KEY}"
      allowed_models: ["*"]

routing:
  default_route: general

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: ["code", "debug", "refactor"]
          exclude: ["code fences", "code block"]
        route: coding

  semantic:
    enabled: true
    provider: ollama
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5

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

    - name: general
      model: llama3
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"
        - "explain how photosynthesis works"

metrics:
  enabled: true

tracing:
  enabled: true
  max_content_length: 200
```

## Next Steps

- [configuration.md](configuration.md) -- full config reference and CLI flags
- [providers.md](providers.md) -- provider details, streaming, and error handling
- [semantic-routing.md](semantic-routing.md) -- threshold tuning, comparison modes, and caching
- [api-keys.md](api-keys.md) -- per-key model and route restrictions
- [ollama-multi-endpoint.md](ollama-multi-endpoint.md) -- weighted load balancing and failover
- [zrok.md](zrok.md) -- overlay networking for sharing and access
- [metrics.md](metrics.md) -- Prometheus instruments and example queries
