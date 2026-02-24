# Configuration

The gateway is configured with a YAML file and optional CLI flags. CLI flags take precedence over the config file.

## Running the Gateway

```bash
llm-gateway run [flags]
```

| Flag | Default | Description |
|---|---|---|
| `-c`, `--config` | `etc/config.yaml` | path to the config file |
| `--address` | (from config) | listen address, overrides `listen` in config |
| `--zrok` | `false` | enable zrok sharing, overrides `zrok.share.enabled` |
| `--zrok-mode` | (from config) | zrok share mode (`public` or `private`), overrides `zrok.share.mode` |

CLI flags override config file values. For example, `--address :9090` will override whatever `listen` is set to in the YAML.

## Config File Format

The config is loaded with `dd.MergeYAMLFile()`, which maps Go struct fields to `snake_case` YAML keys automatically. For example, the struct field `AllowExplicitModel` becomes the YAML key `allow_explicit_model`.

### Environment Variable Substitution

String values in the config support environment variable expansion using `${VAR}` syntax:

```yaml
providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
```

Variables are expanded at config load time using `os.ExpandEnv`.

## Top-Level Keys

```yaml
listen: ":8080"           # HTTP listen address (default: ":8080")

zrok:                     # optional: expose the gateway via zrok
  share:
    enabled: false
    mode: private         # public or private (default: private)
    token: ""             # existing persistent share token (private only)

providers:                # backend provider configs
  open_ai: ...
  anthropic: ...
  ollama: ...

metrics:                  # optional: OpenTelemetry metrics
  enabled: false
  listen: ":9090"         # separate metrics server address

tracing:                  # optional: request body logging
  enabled: false
  max_content_length: 200 # max characters per message (default: 200)

routing:                  # optional: semantic routing
  ...                     # see docs/semantic-routing.md
```

## Provider Configuration

Each provider block is optional. Only configured providers are available for routing. A provider needs at minimum its required credentials (API key for OpenAI/Anthropic) to be initialized.

### OpenAI

```yaml
providers:
  open_ai:
    api_key: "${OPENAI_API_KEY}"      # required
    base_url: "https://api.openai.com" # optional: override for Azure or proxies
    zrok_share_token: ""               # optional: reach the API through a zrok share
```

If `base_url` is omitted, it defaults to `https://api.openai.com`. Setting `base_url` lets you point at Azure OpenAI, a local proxy, or any OpenAI-compatible API.

### Anthropic

```yaml
providers:
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"      # required
    base_url: "https://api.anthropic.com" # optional: override base URL
    zrok_share_token: ""                  # optional: reach the API through a zrok share
```

If `base_url` is omitted, it defaults to `https://api.anthropic.com`.

### Ollama (Single Endpoint)

```yaml
providers:
  ollama:
    base_url: "http://localhost:11434"  # optional (default: http://localhost:11434)
    zrok_share_token: ""                # optional: reach Ollama through a zrok share
```

### Ollama (Multi-Endpoint)

When `endpoints` is present, it replaces `base_url` and `zrok_share_token`. See [docs/ollama-multi-endpoint.md](ollama-multi-endpoint.md) for details.

```yaml
providers:
  ollama:
    endpoints:
      - name: gpu-box-1
        base_url: "http://10.0.0.1:11434"
      - name: gpu-box-2
        base_url: "http://10.0.0.2:11434"
      - name: remote
        zrok_share_token: "abc123"
    health_check:
      interval_seconds: 30   # default: 30
      timeout_seconds: 5     # default: 5
```

### Connecting Providers via Zrok

Any provider can be reached through a zrok share instead of (or alongside) a direct URL. Set `zrok_share_token` on the provider config. The gateway creates a zrok access object that provides an HTTP client routing through the zrok overlay network. See [docs/zrok.md](zrok.md) for details.

## Metrics Configuration

```yaml
metrics:
  enabled: true       # enable OpenTelemetry metrics with Prometheus exporter
  listen: ":9090"     # optional: separate address for the metrics server
```

When enabled, the Prometheus metrics endpoint is served at `GET /metrics` on the main listener. See [docs/metrics.md](metrics.md) for the full list of instruments.

## Tracing Configuration

```yaml
tracing:
  enabled: true             # enable request body logging
  max_content_length: 200   # max characters per message in log output (default: 200)
```

When enabled, each chat completion request is logged with a structured summary showing the requested model, message count, streaming flag, tool count, and each message's role and truncated content. Newlines in message content are escaped to keep each log entry on a single line.

This is useful for debugging semantic routing decisions -- it shows exactly what the client sent, making it easy to identify why a heuristic rule matched or why a request was routed unexpectedly.

## Startup Sequence

1. Load and parse the YAML config file
2. Apply CLI flag overrides
3. Initialize providers (OpenAI, Anthropic, Ollama) in order
4. Create the model-to-provider router
5. Initialize OpenTelemetry metrics (if enabled)
6. Initialize the semantic router (if configured)
7. Start the HTTP server (local or via zrok share)
8. Wait for SIGINT or SIGTERM, then shut down gracefully

On shutdown, the gateway closes all providers, deletes ephemeral zrok shares, and releases zrok access objects.

## Full Example

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
        zrok_share_token: "abc123"

metrics:
  enabled: true

tracing:
  enabled: true
  max_content_length: 300

routing:
  default_route: general

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: ["translate"]
        route: general
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
    - name: coding
      model: claude-haiku-4-5-20251001
      description: "code generation and debugging"
      examples:
        - "write a python function to sort a list"
    - name: general
      model: qwen3-vl:30b
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"
```
