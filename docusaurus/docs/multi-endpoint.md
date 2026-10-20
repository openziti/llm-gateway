---
title: Multi-endpoint load balancing
sidebar_label: Multi-endpoint load balancing
---

# Multi-endpoint load balancing

When you have multiple inference backends, configure the gateway to distribute requests across them
with automatic health checking and failover.

## Supported backends

The gateway works with any OpenAI-compatible backend:

- Ollama
- vLLM
- llama.cpp
- SGLang
- Any server implementing `POST /v1/chat/completions`

## Configuration

Instead of a single `base_url`, define an `endpoints` list:

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
```

Each endpoint has:

- **`name`**: A descriptive name for logging and monitoring.
- **`base_url`**: Direct HTTP access to the backend, or use `zrok_share_token` for overlay network access.
- **`weight`**: Controls traffic distribution proportion. Optional, default `1`.

The `local` key is the section name — it doesn't restrict which backends you can use. Endpoints can be
any OpenAI-compatible server: Ollama, vLLM, llama.cpp, SGLang, or any custom server.

To access a remote backend via zrok:

```yaml
providers:
  local:
    endpoints:
      - name: remote-ollama
        zrok_share_token: ${ZROK_OLLAMA_TOKEN}
        weight: 1
```

## Load balancing

The gateway uses **weighted round-robin** load balancing. An endpoint with `weight: 3` receives roughly
3× the requests of an endpoint with `weight: 1`.

`GET /v1/models` returns the deduplicated union of models from all healthy endpoints.

## Health checking

A background process periodically checks endpoint health:

```yaml
providers:
  local:
    health_check:
      interval_seconds: 30   # check every 30 seconds (default)
      timeout_seconds: 5     # per-endpoint timeout (default)
```

The health check probes `/v1/models` (standard OpenAI format) or falls back to `/api/tags` (Ollama).

When an endpoint fails a check, the gateway logs `endpoint 'name' is now unhealthy` and stops sending it
traffic. When it recovers, it logs `endpoint 'name' is now healthy` and resumes normal traffic. Health
checks continue at an exponential backoff schedule — 1× interval after the first failure, up to 10×
after many failures.

If the system detects a long gap since the last health check (for example, after a VM sleep/wake cycle),
endpoint checks are staggered to avoid flooding the network with simultaneous reconnection attempts.

## Failover

When a request fails due to a network problem (connection refused, timeout, etc.), the gateway retries
on the next healthy endpoint. Application-level errors (HTTP 400, 404, etc.) don't trigger failover —
they indicate a problem with the request, not the endpoint.

## Semantic routing integration

When semantic routing uses the local provider in multi-endpoint mode, embedding and classifier requests
automatically benefit from the same load distribution and failover via a shared HTTP client.
No additional configuration is needed.

## Full example

Three local endpoints with weighted distribution, health checking, and a zrok-connected backup alongside cloud providers:

```yaml
providers:
  local:
    endpoints:
      - name: ollama-primary
        base_url: http://localhost:11434
        weight: 3
      - name: ollama-secondary
        base_url: http://localhost:11435
        weight: 1
      - name: vllm-prod
        base_url: http://vllm-prod.example.com:8000
        weight: 2
      - name: vllm-backup
        zrok_share_token: ${ZROK_VLLM_BACKUP_TOKEN}
        weight: 1

    health_check:
      interval_seconds: 30
      timeout_seconds: 5

  open_ai:
    api_key: ${OPENAI_API_KEY}

  anthropic:
    api_key: ${ANTHROPIC_API_KEY}
```
