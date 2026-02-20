# Multi-Endpoint Ollama

When you have multiple Ollama instances (e.g., several GPU machines), the gateway can distribute requests across them with round-robin load balancing, health checking, and automatic failover.

## Configuration

Replace the single `base_url`/`zrok_share_token` with an `endpoints` list:

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
      interval_seconds: 30
      timeout_seconds: 5
```

Each endpoint has a `name` (used in logs and metrics) and either a `base_url` for direct HTTP or a `zrok_share_token` for overlay access. You can mix both in the same config.

### Health Check Settings

| Key | Default | Description |
|---|---|---|
| `interval_seconds` | 30 | seconds between health check rounds |
| `timeout_seconds` | 5 | per-endpoint timeout for the health check request |

## How It Works

### Round-Robin Selection

Requests are distributed across healthy endpoints using a round-robin counter. Each call to the provider advances the counter and picks the next healthy endpoint in order.

If all endpoints are unhealthy, the first endpoint is used as a best-effort fallback.

### Health Checking

A background goroutine periodically sends `GET /api/tags` to each endpoint. This is a lightweight Ollama endpoint that lists installed models -- it exercises the network path and confirms the Ollama process is running.

An endpoint is marked **unhealthy** when:
- The health check request fails (connection refused, timeout, DNS error)
- The health check returns a non-200 status code

An endpoint is marked **healthy** when:
- The health check returns 200

Health state transitions are logged:

```
endpoint 'gpu-box-1' is now unhealthy
endpoint 'gpu-box-1' is now healthy
```

An initial health check runs immediately at startup, before any requests are served.

### Failover

When a chat completion or streaming request fails with a **network error** (connection refused, timeout, DNS failure), the endpoint is marked unhealthy and the request is retried on the next healthy endpoint. This continues until either a request succeeds or all endpoints have been tried.

**Application-level errors** (HTTP 400, 404, model not found, etc.) are **not** retried. These indicate a problem with the request itself, not the endpoint, so failing over would just repeat the same error.

If all endpoints fail with network errors, the gateway returns the last error to the client.

### Model Listing

`GET /v1/models` queries all healthy endpoints and returns the deduplicated union of their model lists. If an endpoint fails to respond, it is skipped. If no endpoints respond, the last error is returned.

This means clients see every model available across the fleet, regardless of which specific machine hosts it.

## Round-Robin Client

When semantic routing is configured with `provider: ollama` and Ollama is in multi-endpoint mode, the embedding and classifier layers use a **round-robin HTTP client** that distributes their requests across the same endpoint pool.

This client works as a custom `http.RoundTripper` that:
1. Selects the next healthy endpoint via round-robin
2. Rewrites the request URL to target that endpoint
3. Uses the endpoint's own HTTP transport (supporting zrok-based endpoints)
4. On network errors, marks the endpoint unhealthy and retries with the next one

This means embedding and classifier requests benefit from the same load distribution and failover as chat completions, without any additional configuration.

## Example: Two Local GPUs and One Remote

```yaml
providers:
  ollama:
    endpoints:
      - name: local-3090
        base_url: "http://localhost:11434"
      - name: local-4090
        base_url: "http://192.168.1.50:11434"
      - name: cloud-a100
        zrok_share_token: "a100-share"
    health_check:
      interval_seconds: 15
      timeout_seconds: 3
```

Requests rotate across all three. If the cloud machine goes offline, health checks detect the failure and requests are distributed across the two local machines until it recovers.
