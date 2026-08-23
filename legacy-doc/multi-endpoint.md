# Multi-Endpoint Load Balancing

When you have multiple inference backends (e.g., several GPU machines running Ollama, vLLM, llama-server, or any OpenAI-compatible server), the gateway can distribute requests across them with weighted round-robin load balancing, health checking, and automatic failover.

## Backend Compatibility

The multi-endpoint layer sends chat completions to `POST {base_url}/v1/chat/completions` -- the standard OpenAI-compatible endpoint. Any backend that implements this endpoint works as a multi-endpoint target:

- [Ollama](https://ollama.com)
- [llama.cpp / llama-server](https://github.com/ggerganov/llama.cpp)
- [vLLM](https://github.com/vllm-project/vllm)
- [SGLang](https://github.com/sgl-project/sglang)
- Any other OpenAI-compatible inference server

The `local` config key is the section name -- it does not restrict which backends you can use.

## Configuration

Replace the single `base_url`/`zrok_share_token` with an `endpoints` list:

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
      interval_seconds: 30
      timeout_seconds: 5
```

Each endpoint has a `name` (used in logs and metrics) and either a `base_url` for direct HTTP or a `zrok_share_token` for overlay access. You can mix both in the same config.

The optional `weight` field (default: 1) controls the proportion of traffic each endpoint receives. An endpoint with `weight: 3` gets roughly 3x the requests of an endpoint with `weight: 1`.

### Health Check Settings

| Key | Default | Description |
|---|---|---|
| `interval_seconds` | 30 | seconds between health check rounds |
| `timeout_seconds` | 5 | per-endpoint timeout for the health check request |

## How It Works

### Weighted Round-Robin Selection

Requests are distributed across healthy endpoints using a weighted round-robin counter. Each endpoint appears in the internal rotation proportionally to its weight -- an endpoint with `weight: 3` appears three times for every one appearance of a `weight: 1` endpoint. Each call to the provider advances the counter and picks the next healthy endpoint in order.

If all endpoints are unhealthy, the first endpoint is used as a best-effort fallback.

### Health Checking

A background goroutine periodically probes each endpoint to verify it is running. The probe tries `GET /v1/models` first (the standard OpenAI-compatible endpoint), falling back to `GET /api/tags` (Ollama's native endpoint). This means health checks work regardless of which backend software the endpoint runs.

An endpoint is marked **unhealthy** when:
- The health check request fails (connection refused, timeout, DNS error)
- The health check returns a non-200 status code on both probe paths

An endpoint is marked **healthy** when:
- Either probe returns 200

Failing endpoints are rechecked with exponential backoff -- each consecutive failure increases the delay by one interval, up to 10x the base interval. This avoids hammering infrastructure that is down or rate-limiting. The backoff resets when the endpoint recovers.

Health state transitions are logged:

```
endpoint 'gpu-box-1' is now unhealthy
endpoint 'gpu-box-1' is now healthy
```

An initial health check runs immediately at startup, before any requests are served.

If the system detects a long gap since the last health check (e.g., after a VM sleep/wake cycle), endpoint checks are staggered to avoid flooding the network with simultaneous reconnection attempts.

### Failover

When a chat completion or streaming request fails with a **network error** (connection refused, timeout, DNS failure), the endpoint is marked unhealthy and the request is retried on the next healthy endpoint. This continues until either a request succeeds or all endpoints have been tried.

**Application-level errors** (HTTP 400, 404, model not found, etc.) are **not** retried. These indicate a problem with the request itself, not the endpoint, so failing over would just repeat the same error.

If all endpoints fail with network errors, the gateway returns the last error to the client.

### Model Listing

`GET /v1/models` queries all healthy endpoints and returns the deduplicated union of their model lists. If an endpoint fails to respond, it is skipped. If no endpoints respond, the last error is returned.

This means clients see every model available across the fleet, regardless of which specific machine hosts it.

## Round-Robin Client

When semantic routing is configured with `provider: local` and the local provider is in multi-endpoint mode, the embedding and classifier layers use a **round-robin HTTP client** that distributes their requests across the same endpoint pool.

This client works as a custom `http.RoundTripper` that:
1. Selects the next healthy endpoint via round-robin
2. Rewrites the request URL to target that endpoint
3. Uses the endpoint's own HTTP transport (supporting zrok-based endpoints)
4. On network errors, marks the endpoint unhealthy and retries with the next one

This means embedding and classifier requests benefit from the same load distribution and failover as chat completions, without any additional configuration.

## Example: Two Local GPUs and One Remote

```yaml
providers:
  local:
    endpoints:
      - name: local-3090
        base_url: "http://localhost:11434"
      - name: local-4090
        base_url: "http://192.168.1.50:11434"
        weight: 2
      - name: cloud-a100
        zrok_share_token: "a100-share"
        weight: 3
    health_check:
      interval_seconds: 15
      timeout_seconds: 3
```

With these weights, out of every 6 requests roughly 1 goes to the 3090, 2 to the 4090, and 3 to the A100. If the cloud machine goes offline, health checks detect the failure and requests are distributed across the two local machines until it recovers.

## Example: Mixed Backends

The endpoints don't all have to run the same software:

```yaml
providers:
  local:
    endpoints:
      - name: ollama-box
        base_url: "http://10.0.0.1:11434"
      - name: vllm-server
        base_url: "http://10.0.0.2:8000"
        weight: 2
      - name: llama-server
        base_url: "http://10.0.0.3:8080"
    health_check:
      interval_seconds: 30
      timeout_seconds: 5
```

The gateway sends `POST /v1/chat/completions` to each endpoint regardless of what software is behind the URL. The load balancing and failover layer doesn't care what's behind the URL.
