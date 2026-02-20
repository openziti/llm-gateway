# Zrok Integration

The gateway uses [zrok](https://zrok.io) in two independent ways:

1. **Sharing** -- exposing the gateway itself over a zrok share so clients can reach it without a public IP or open ports.
2. **Accessing** -- connecting to backend providers (OpenAI, Anthropic, Ollama) through zrok shares instead of direct HTTP.

Both use zrok's overlay network built on [OpenZiti](https://openziti.io). The machine running the gateway must have a zrok environment enabled (`zrok enable`).

## Sharing the Gateway

Instead of listening on a TCP port, the gateway can serve traffic through a zrok share. Clients connect to the share token rather than an IP address.

### Ephemeral Shares

An ephemeral share is created at startup and deleted when the gateway shuts down.

```yaml
zrok:
  share:
    enabled: true
    mode: private    # or public
```

Or via CLI flags:

```bash
llm-gateway run --zrok --zrok-mode private
```

The share token is logged at startup:

```
serving via zrok share 'abc123def456'
```

**Public mode** creates a share accessible by anyone with the token. **Private mode** (the default) requires the client to also have a zrok environment enabled and creates an access-controlled connection through the overlay.

### Persistent Shares

Ephemeral shares get a new token on every restart. If you need a stable token, create a persistent share externally with `zrok reserve` and pass its token to the gateway:

```yaml
zrok:
  share:
    enabled: true
    token: "abc123"    # existing persistent share token
```

Persistent shares are always private. The gateway connects a listener to the existing share but does not delete it on shutdown -- the share is managed externally.

### How It Works

When sharing is enabled, the gateway replaces its normal TCP listener with a zrok listener. The HTTP server's `Serve()` method receives connections from the overlay network instead of from a local socket. From the handler's perspective, nothing changes -- it still receives `http.Request` objects and writes `http.Response` objects.

On shutdown (SIGINT/SIGTERM), the gateway:
1. Gracefully drains the HTTP server
2. Closes the zrok listener
3. Deletes the share (ephemeral only)

## Accessing Providers via Zrok

Any provider can be reached through a zrok share by setting `zrok_share_token` in its config. This is useful when a provider runs on a different machine that isn't directly reachable over the network but is connected to the same zrok environment.

```yaml
providers:
  ollama:
    zrok_share_token: "remote-ollama-token"

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    zrok_share_token: "anthropic-proxy-token"
```

### How It Works

For each provider with a `zrok_share_token`, the gateway creates a **zrok access** object at startup. This access provides an `http.Client` whose `DialContext` function routes connections through the zrok overlay to the share, bypassing normal DNS and TCP routing.

The provider uses this custom HTTP client for all its API calls. The `base_url` field still determines the URL path structure, but the actual network connection goes through zrok. If `base_url` is omitted, the provider's default is used (e.g., `https://api.openai.com` for OpenAI, `http://localhost:11434` for Ollama).

On shutdown, all access objects are deleted to clean up zrok resources.

### Multi-Endpoint Ollama

Each Ollama endpoint can independently use zrok or direct HTTP:

```yaml
providers:
  ollama:
    endpoints:
      - name: local
        base_url: "http://localhost:11434"
      - name: remote-gpu
        zrok_share_token: "gpu-box-token"
```

Each endpoint with a `zrok_share_token` gets its own zrok access and HTTP client. The round-robin load balancer uses whichever transport (direct or zrok) is configured per endpoint.

### Embedding and Classifier Providers

When semantic routing is configured with `provider: ollama` and Ollama is in multi-endpoint mode, embedding and classifier requests are sent through the same round-robin client used for chat completions. This means they automatically benefit from the same load distribution and failover behavior.

## Prerequisites

The gateway requires a zrok environment on the host machine. If `zrok enable` hasn't been run, the gateway will fail at startup with:

```
zrok environment is not enabled; run 'zrok enable' first
```

This applies to both sharing and accessing. Each zrok operation (creating a share, creating an access) loads the environment root from the standard zrok config location.
