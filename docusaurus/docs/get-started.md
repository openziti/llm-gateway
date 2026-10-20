---
title: Get started with NetFoundry LLM Gateway
sidebar_label: Get started
---

# Get started

This guide walks you through installing NetFoundry LLM Gateway and running your first requests. By the
end, you'll have the gateway proxying requests to one or more inference providers.

## Installation

Choose the installation method that fits your environment.

### Pre-built binaries

Pre-built binaries are available for Linux, macOS, and Windows:

1. Visit the [GitHub Releases](https://github.com/openziti/llm-gateway/releases) page.
2. Download the binary for your platform.
3. Make it executable:

   ```bash
   chmod +x llm-gateway
   ```

4. Run it:

   ```bash
   ./llm-gateway run config.yaml
   ```

### Install with Go

If you have Go 1.22 or later:

```bash
go install github.com/openziti/llm-gateway/cmd/llm-gateway@latest
llm-gateway run config.yaml
```

### Build from source

Clone the repository and build the binary locally:

```bash
git clone https://github.com/openziti/llm-gateway.git
cd llm-gateway
go build -o llm-gateway ./cmd/llm-gateway
./llm-gateway run config.yaml
```

## Examples

The examples below progress from a simple single-provider proxy to a full production configuration.

### Proxy a local inference server

1. Start Ollama:

    ```bash
    ollama serve
    ```

2. Create `config.yaml`:

    ```yaml
    local:
      base_url: http://localhost:11434
    ```

3. Start the gateway:

    ```bash
    llm-gateway run config.yaml
    ```

4. Send a request:

    ```bash
    curl -X POST http://localhost:8080/v1/chat/completions \
      -H "Content-Type: application/json" \
      -d '{
        "model": "llama2",
        "messages": [{"role": "user", "content": "Hello"}],
        "temperature": 0.7
      }'
    ```

The gateway listens on `http://localhost:8080` by default.

### Route between OpenAI and Anthropic

The gateway routes requests to the correct provider by prefix-matching on the model name:

```yaml
providers:
  open_ai:
    api_key: ${OPENAI_API_KEY}

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}

  local:
    base_url: http://localhost:11434
```

Requests are routed automatically based on the model prefix: `gpt-*` goes to OpenAI, `claude-*` to
Anthropic, everything else to the local provider:

```bash
# Routes to OpenAI
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello from OpenAI"}]}'

# Routes to Anthropic
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-3-sonnet-20240229", "messages": [{"role": "user", "content": "Hello from Anthropic"}]}'

# Routes to local Ollama
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "llama2", "messages": [{"role": "user", "content": "Hello from Ollama"}]}'
```

### Restrict API access with virtual keys

1. Generate an API key:

    ```bash
    llm-gateway genkey
    # sk-gw-a1b2c3d4e5f6...
    ```

2. Add `api_keys` to your config, referencing the key and setting per-key model permissions:

    ```yaml
    api_keys:
      enabled: true
      keys:
        - name: primary
          key: ${PRIMARY_API_KEY}
          allowed_models: ["gpt-*", "claude-*"]
        - name: local-only
          key: ${LOCAL_API_KEY}
          allowed_models: ["llama*"]

    providers:
      open_ai:
        api_key: ${OPENAI_API_KEY}

      anthropic:
        api_key: ${ANTHROPIC_API_KEY}

      local:
        base_url: http://localhost:11434
    ```

3. Clients send their key in the `Authorization` header:

    ```bash
    curl -X POST http://localhost:8080/v1/chat/completions \
      -H "Content-Type: application/json" \
      -H "Authorization: Bearer sk-gw-a1b2c3d4e5f6..." \
      -d '{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}'
    ```

### Use the Python OpenAI client

The gateway works as a drop-in replacement for the OpenAI Python client. Point `base_url` at the
gateway and it handles provider routing transparently:

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

### Semantic routing

Route requests automatically based on content analysis, without requiring clients to specify a model:

```yaml
routing:
  default_route: general
  semantic:
    enabled: true
    provider: local
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5
  routes:
    - name: coding
      model: claude-haiku-4-5-20251001
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
        - "debug this segfault in my C code"
    - name: general
      model: qwen3-vl:30b
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"
        - "explain how photosynthesis works"

providers:
  local:
    base_url: http://localhost:11434
```

See [Semantic routing](semantic-routing.md) for a full explanation of how routing works.

### Multi-endpoint load balancing

Distribute requests across multiple inference backends:

```yaml
providers:
  local:
    endpoints:
      - name: ollama-primary
        base_url: http://localhost:11434
        weight: 2
      - name: ollama-secondary
        base_url: http://localhost:11435
        weight: 1
      - name: vllm-endpoint
        base_url: http://vllm.example.com:8000
        weight: 1

  open_ai:
    api_key: ${OPENAI_API_KEY}

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
```

See [Multi-endpoint load balancing](multi-endpoint.md) for health check and failover options.

### Connect via zrok

Share the gateway over a zrok overlay so clients can reach it without a public IP:

```yaml
zrok:
  share:
    enabled: true
    mode: private

providers:
  local:
    base_url: http://localhost:11434
```

Or access a remote inference backend through a zrok share:

```yaml
providers:
  local:
    endpoints:
      - name: remote-ollama
        zrok_share_token: ${ZROK_OLLAMA_TOKEN}
```

See [Connect via zrok](connect-zrok.md) for setup details.

### Production configuration

A full configuration combining multiple providers, API key authentication, semantic routing, load
balancing, metrics, and zrok:

```yaml
listen: "0.0.0.0:8080"

zrok:
  share:
    enabled: true
    token: ${ZROK_SHARE_TOKEN}

api_keys:
  enabled: true
  keys:
    - name: primary
      key: ${PRIMARY_API_KEY}
      allowed_models: ["gpt-*", "claude-*", "llama*"]
    - name: local-only
      key: ${LOCAL_API_KEY}
      allowed_models: ["llama*"]

providers:
  open_ai:
    api_key: ${OPENAI_API_KEY}

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}

  local:
    endpoints:
      - name: ollama-primary
        base_url: http://localhost:11434
        weight: 3
      - name: ollama-secondary
        base_url: http://localhost:11435
        weight: 1
      - name: vllm-endpoint
        zrok_share_token: ${ZROK_VLLM_TOKEN}
        weight: 2

routing:
  default_route: general
  semantic:
    enabled: true
    provider: local
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5
  routes:
    - name: coding
      model: claude-haiku-4-5-20251001
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
    - name: general
      model: qwen3-vl:30b
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"

metrics:
  enabled: true
```

## More info

- [Configuration](configuration.md): All configuration options
- [Providers](providers.md): How provider routing and format translation work
- [Multi-endpoint load balancing](multi-endpoint.md): Advanced load balancing strategies
- [Virtual API keys](api-keys.md): Client authentication and model-level restrictions
- [Semantic routing](semantic-routing.md): Intelligent request routing based on content
- [Metrics](metrics.md): Prometheus metrics and observability
