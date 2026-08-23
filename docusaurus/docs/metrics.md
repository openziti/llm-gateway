---
title: Metrics
sidebar_label: Metrics
---

# Metrics

The gateway exposes OpenTelemetry metrics via a Prometheus exporter. When enabled, metrics are available
at `GET /metrics` in the standard Prometheus text format.

## Enabling metrics

Add the following to your config file:

```yaml
metrics:
  enabled: true
```

## Instruments

All metric names are prefixed with `llm_gateway.`.

### Request metrics

These metrics track individual requests through the gateway:

**`llm_gateway.requests`** (counter): Total chat completion requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | `openai`, `anthropic`, `ollama` | Which provider handled the request |
| `model` | Model name | The model used |
| `streaming` | `true`, `false` | Whether the request was streaming |
| `key` | Key name or empty | The API key name (when [virtual API keys](api-keys.md) are enabled) |

---

**`llm_gateway.request.duration`** (histogram, seconds): End-to-end request duration including
upstream provider latency.

| Attribute | Values | Description |
|---|---|---|
| `provider` | `openai`, `anthropic`, `ollama` | Which provider handled the request |
| `model` | Model name | The model used |
| `key` | Key name or empty | The API key name (when [virtual API keys](api-keys.md) are enabled) |

---

**`llm_gateway.requests.inflight`** (up-down counter): Number of requests currently being processed.
Incremented when a request enters the handler, decremented when it completes. Useful for understanding
concurrency and detecting request pileups. No attributes.

### Token metrics

These metrics track token consumption as reported by each provider:

**`llm_gateway.tokens.prompt`** (counter): Total prompt (input) tokens across all requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | Provider name | Which provider reported the usage |
| `model` | Model name | The model used |

---

**`llm_gateway.tokens.completion`** (counter): Total completion (output) tokens across all requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | Provider name | Which provider reported the usage |
| `model` | Model name | The model used |

Token metrics are recorded from the `usage` field in non-streaming responses. Streaming responses
typically don't include token counts.

### Routing metrics

This metric tracks how routing decisions are distributed across the cascade layers:

**`llm_gateway.routing.decisions`** (counter): Semantic routing decisions, counted each time the
router selects a model.

| Attribute | Values | Description |
|---|---|---|
| `method` | `explicit`, `heuristic`, `semantic`, `classifier`, `default` | Which routing layer made the decision |

A high proportion of `default` decisions may indicate that thresholds are too strict or that route
examples don't cover your traffic well.

### Error metrics

This metric tracks errors returned by upstream providers, broken down by error category:

**`llm_gateway.provider.errors`** (counter): Errors returned by upstream providers.

| Attribute | Values | Description |
|---|---|---|
| `error_type` | `invalid_request_error`, `authentication_error`, `rate_limit_error`, `server_error`, `not_found_error`, `service_unavailable`, `unknown` | The error category |

### Health metrics

This metric tracks endpoint availability in multi-endpoint mode:

**`llm_gateway.endpoint.healthy`** (up-down counter): Per-endpoint health status. Value is `1` for
healthy endpoints and `0` for unhealthy endpoints.

| Attribute | Values | Description |
|---|---|---|
| `endpoint` | Endpoint name | The endpoint being reported on |

## Prometheus scraping

Point your Prometheus instance at the gateway's `/metrics` endpoint:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: llm-gateway
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:8080"]
```

## Useful queries

Some example PromQL queries to get started:

- Requests per minute by provider:

  ```promql
  rate(llm_gateway_requests_total[5m]) * 60
  ```

- Average request duration by model:

  ```promql
  rate(llm_gateway_request_duration_seconds_sum[5m]) / rate(llm_gateway_request_duration_seconds_count[5m])
  ```

- Token throughput (tokens per second):

  ```promql
  rate(llm_gateway_tokens_prompt_total[5m]) + rate(llm_gateway_tokens_completion_total[5m])
  ```

- Error rate as a percentage of total requests:

  ```promql
  rate(llm_gateway_provider_errors_total[5m]) / rate(llm_gateway_requests_total[5m]) * 100
  ```

- Routing method distribution:

  ```promql
  rate(llm_gateway_routing_decisions_total[5m])
  ```

- Current in-flight requests:

  ```promql
  llm_gateway_requests_inflight
  ```
