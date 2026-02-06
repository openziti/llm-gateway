# llm-gateway

An OpenAI-compatible API proxy that routes requests to OpenAI, Anthropic, or Ollama backends. Optionally expose the gateway via [zrok](https://zrok.io) for zero-trust access.

## Features

- **OpenAI-compatible API**: Drop-in replacement for OpenAI client libraries
- **Multi-provider routing**: Automatically routes requests based on model name
- **Anthropic translation**: Transparently converts OpenAI format to/from Anthropic's Messages API
- **Streaming support**: Server-Sent Events (SSE) streaming for all providers
- **zrok integration**: Expose the gateway via zrok private or public shares
- **Zero-trust backends**: Connect to Ollama instances via zrok (no exposed ports)

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
  openai:
    apiKey: "${OPENAI_API_KEY}"
  anthropic:
    apiKey: "${ANTHROPIC_API_KEY}"
  ollama:
    baseURL: "http://localhost:11434"
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

OpenAI-compatible chat completions endpoint. Supports both streaming (`stream: true`) and non-streaming requests.

### GET /v1/models

Returns available models from all configured providers.

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
  openai:
    apiKey: "${OPENAI_API_KEY}"
    baseURL: ""        # optional: override for Azure or compatible APIs

  anthropic:
    apiKey: "${ANTHROPIC_API_KEY}"
    baseURL: ""        # optional: override base URL

  ollama:
    baseURL: "http://localhost:11434"
    zrokShare: ""      # optional: connect via zrok share token
```

### Environment Variables

API keys support environment variable expansion using `${VAR}` syntax.

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

### Connecting to Ollama via zrok

Run Ollama as a zrok share (zero exposed ports), then connect:

```yaml
providers:
  ollama:
    zrokShare: "ollama-share-token"
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
