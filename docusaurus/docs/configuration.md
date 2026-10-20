---
title: Configuration reference
sidebar_label: Configuration
---

# Configuration reference

NetFoundry LLM Gateway is configured with a YAML file. CLI flags can override individual settings.

## Gateway settings

Controls the listen address for the gateway process:

```yaml
listen: ":8080"   # address to listen on (default: :8080)
```

To expose the gateway over a zrok overlay instead of a local port, add a top-level `zrok:` block:

```yaml
zrok:
  share:
    enabled: false
    mode: private
    token: ""
```

## Providers

Configure which inference providers the gateway can route to:

```yaml
providers:
  open_ai:
    api_key: ${OPENAI_API_KEY}    # supports environment variable expansion

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}

  local:
    base_url: http://localhost:11434
```

## Virtual API keys

Restrict client access with named keys and per-key model permissions:

```yaml
api_keys:
  enabled: true
  keys:
    - name: alice
      key: ${ALICE_KEY}
      allowed_models: ["gpt-*", "claude-*"]
    - name: bob
      key: ${BOB_KEY}
      allowed_models: ["llama*"]
```

See [Virtual API keys](api-keys.md) for a full reference.

## Routing

Enable semantic routing and define named routes:

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
```

See [Semantic routing](semantic-routing.md) for a full reference.

## Metrics

Expose a Prometheus metrics endpoint:

```yaml
metrics:
  enabled: true
```

## Tracing

Enable request body logging for debugging routing decisions:

```yaml
tracing:
  enabled: true
  max_content_length: 200   # max characters per message in log output
```

When enabled, each chat completion request is logged with the model, message count, streaming flag,
tool count, and each message's role and truncated content.

## Environment variables

String values support `${VAR_NAME}` expansion. Variables are expanded at startup:

```bash
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...
llm-gateway run config.yaml
```

## Complete example

A full configuration combining all sections:

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

providers:
  open_ai:
    api_key: ${OPENAI_API_KEY}

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}

  local:
    base_url: http://localhost:11434

routing:
  default_route: general
  semantic:
    enabled: true
    provider: local
    model: nomic-embed-text
    threshold: 0.75

metrics:
  enabled: true
```

## Run the gateway

Pass the config file path as the first argument:

```bash
llm-gateway run config.yaml
```

## CLI flags

```
llm-gateway run <config-path> [flags]

Flags:
  --address string   Gateway listen address (e.g., 0.0.0.0:8080)
  --zrok             Enable zrok share (boolean)
  --zrok-mode string Zrok share mode (private or public)
  -h, --help         Show help
```

CLI flags take precedence over the config file.

## Startup sequence

When the gateway starts, it:

1. Loads and parses the YAML config file.
2. Applies any CLI flag overrides.
3. Expands environment variables.
4. Initializes providers (OpenAI, Anthropic, local/self-hosted) in order.
5. Creates the model-to-provider router.
6. Initializes OpenTelemetry metrics (if enabled).
7. Initializes the semantic router (if configured).
8. Starts the HTTP server (local or via zrok share).

On shutdown (SIGINT/SIGTERM), the gateway closes all providers, deletes ephemeral zrok shares, and
releases zrok access objects before exiting.
