# Metrics

The gateway exposes OpenTelemetry metrics via a Prometheus exporter. When enabled, metrics are available at `GET /metrics` in the standard Prometheus text format.

## Enabling Metrics

```yaml
metrics:
  enabled: true
```

## Instruments

All metric names are prefixed with `llm_gateway.`.

### Request Metrics

**`llm_gateway.requests`** (counter) -- total chat completion requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | `openai`, `anthropic`, `ollama` | which provider handled the request |
| `model` | model name | the model used |
| `streaming` | `true`, `false` | whether the request was streaming |

**`llm_gateway.request.duration`** (histogram, seconds) -- end-to-end request duration including upstream provider latency.

| Attribute | Values | Description |
|---|---|---|
| `provider` | `openai`, `anthropic`, `ollama` | which provider handled the request |
| `model` | model name | the model used |

**`llm_gateway.requests.inflight`** (up-down counter) -- number of requests currently being processed. Incremented when a request enters the handler, decremented when it completes. Useful for understanding concurrency and detecting request pileups.

### Token Metrics

**`llm_gateway.tokens.prompt`** (counter) -- total prompt (input) tokens across all requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | provider name | which provider reported the usage |
| `model` | model name | the model used |

**`llm_gateway.tokens.completion`** (counter) -- total completion (output) tokens across all requests.

| Attribute | Values | Description |
|---|---|---|
| `provider` | provider name | which provider reported the usage |
| `model` | model name | the model used |

Token metrics are recorded from the `usage` field in non-streaming chat completion responses. Streaming responses typically do not include token counts.

### Routing Metrics

**`llm_gateway.routing.decisions`** (counter) -- semantic routing decisions, counted each time the router selects a model.

| Attribute | Values | Description |
|---|---|---|
| `method` | `explicit`, `heuristic`, `semantic`, `classifier`, `default` | which routing layer made the decision |

A high proportion of `default` decisions may indicate that thresholds are too strict or that route examples don't cover your traffic well.

### Error Metrics

**`llm_gateway.provider.errors`** (counter) -- errors returned by upstream providers.

| Attribute | Values | Description |
|---|---|---|
| `error_type` | `invalid_request_error`, `authentication_error`, `rate_limit_error`, `server_error`, `not_found_error`, `service_unavailable`, `unknown` | the error category |

### Health Metrics

**`llm_gateway.endpoint.healthy`** (up-down counter) -- per-endpoint health status for multi-endpoint Ollama. Value is 1 for healthy endpoints and 0 for unhealthy endpoints. The `endpoint` attribute identifies the endpoint by name.

## Prometheus Scraping

Point your Prometheus instance at the gateway's `/metrics` endpoint:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: llm-gateway
    scrape_interval: 15s
    static_configs:
      - targets: ["localhost:8080"]
```

## Useful Queries

Requests per minute by provider:

```promql
rate(llm_gateway_requests_total[5m]) * 60
```

Average request duration by model:

```promql
rate(llm_gateway_request_duration_seconds_sum[5m]) / rate(llm_gateway_request_duration_seconds_count[5m])
```

Token throughput (tokens per second):

```promql
rate(llm_gateway_tokens_prompt_total[5m]) + rate(llm_gateway_tokens_completion_total[5m])
```

Error rate as a percentage of total requests:

```promql
rate(llm_gateway_provider_errors_total[5m]) / rate(llm_gateway_requests_total[5m]) * 100
```

Routing method distribution:

```promql
rate(llm_gateway_routing_decisions_total[5m])
```

Current in-flight requests:

```promql
llm_gateway_requests_inflight
```
