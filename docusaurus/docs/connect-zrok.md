---
title: Connect via zrok
sidebar_label: Connect via zrok
---

# Connect via zrok

The gateway uses [zrok](https://zrok.io) in two independent ways:

- **Sharing**: Exposes the gateway over a zrok share so clients can reach it without a public IP or
  open ports.
- **Accessing**: Connects to backend providers through zrok shares instead of direct HTTP.

Both use zrok's overlay network built on [OpenZiti](https://openziti.io).

## Prerequisites

The gateway requires a zrok environment on the host machine. If `zrok enable` hasn't been run, the
gateway fails at startup:

```
zrok environment is not enabled; run 'zrok enable' first
```

This applies to both sharing and accessing.

## Share the gateway

Instead of listening on a TCP port, the gateway can serve traffic through a zrok share. Clients connect
to the share token rather than an IP address.

### Ephemeral shares

An ephemeral share is created at startup and deleted when the gateway shuts down.

1. Add the zrok config to `config.yaml`:

    ```yaml
    zrok:
      share:
        enabled: true
        mode: private    # or public
    ```

    Alternatively, pass flags at runtime:

    ```bash
    llm-gateway run config.yaml --zrok --zrok-mode private
    ```

2. Start the gateway. The share token is logged at startup:

    ```
    serving via zrok share 'abc123def456'
    ```

3. Give clients the share token to connect.

**Public mode** creates a share accessible by anyone with the token. **Private mode** (the default)
requires the client to have a zrok environment enabled and creates an access-controlled connection
through the overlay.

### Persistent shares

Ephemeral shares get a new token on every restart. For a stable token, create a persistent share with
`zrok reserve` and pass its token to the gateway:

```yaml
zrok:
  share:
    enabled: true
    token: "abc123"    # existing persistent share token
```

Persistent shares are always private. The gateway connects to the existing share but doesn't delete it
on shutdown — the share is managed externally.

## Access providers via zrok

Any provider can be reached through a zrok share by setting `zrok_share_token` in its config. This is
useful when a provider runs on a different machine that isn't directly reachable over the network but
is connected to the same zrok environment:

```yaml
providers:
  local:
    zrok_share_token: "remote-ollama-token"

  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    zrok_share_token: "anthropic-proxy-token"
```

### Multi-endpoint

Each endpoint can independently use zrok or direct HTTP:

```yaml
providers:
  local:
    endpoints:
      - name: local
        base_url: "http://localhost:11434"
      - name: remote-gpu
        zrok_share_token: "gpu-box-token"
```

Each endpoint with a `zrok_share_token` gets its own zrok access and HTTP client. The round-robin
load balancer uses whichever transport is configured per endpoint.
