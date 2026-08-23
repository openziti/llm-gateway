---
title: Providers
sidebar_label: Providers
---

# Providers

The gateway presents a single OpenAI-compatible API to clients and translates requests to the
appropriate backend provider. Three provider types are supported: OpenAI (and compatible APIs),
Anthropic, and a local/self-hosted provider for any backend that implements `/v1/chat/completions`.

## API surface

All clients interact with the gateway using the [OpenAI chat completions format](https://developers.openai.com/api/reference/chat-completions/overview):

```
POST /v1/chat/completions    chat completions (streaming and non-streaming)
GET  /v1/models              list available models from all providers
GET  /health                 health check
GET  /metrics                Prometheus metrics (when enabled)
```

By default, the gateway doesn't require a client API key — authentication is between the gateway and the
upstream providers. Optionally, the gateway can enforce its own [virtual API keys](api-keys.md).

## Model routing

Models are routed to providers by prefix-matching on the model name:

| Prefix | Provider |
|---|---|
| `gpt-*`, `o1-*`, `o3-*` | OpenAI |
| `claude-*` | Anthropic |
| Everything else | Local (configured as `local`) |

Matching is case-insensitive. A request for `gpt-4` goes to OpenAI; `claude-haiku-4-5-20251001` goes to
Anthropic; `llama3` or `qwen3-vl:30b` go to the local provider.

If the target provider isn't configured, the gateway returns an error:

```json
{"error": {"message": "provider 'openai' is not configured", "type": "invalid_request_error"}}
```

## OpenAI provider

The OpenAI provider is a direct pass-through. Requests forward to `POST {base_url}/v1/chat/completions`
with an `Authorization: Bearer` header. Responses are returned unmodified.

Any OpenAI-compatible API can be used as the OpenAI provider by setting `base_url` — for example,
Azure OpenAI or a local vLLM server.

Model listing calls `GET {base_url}/v1/models`.

## Anthropic provider

The Anthropic provider translates between the OpenAI format and
[Anthropic's Messages API](https://docs.anthropic.com/en/docs/api-reference/messages/create). Clients
send OpenAI-format requests and receive OpenAI-format responses regardless of which provider handles
the request.

### Request translation

The gateway maps OpenAI request fields to their Anthropic equivalents before forwarding:

| OpenAI field | Anthropic field | Notes |
|---|---|---|
| `model` | `model` | Passed through |
| `messages` (role: system) | `system` | First system message becomes Anthropic's top-level `system` field |
| `messages` (role: user) | `messages` (role: user) | |
| `messages` (role: assistant) | `messages` (role: assistant) | |
| `messages` (role: tool) | `messages` (role: user) | Mapped to user role |
| `max_tokens` | `max_tokens` | Defaults to 4096 if not set (Anthropic requires this field) |
| `temperature` | `temperature` | |
| `top_p` | `top_p` | |
| `stop` | `stop_sequences` | String or array |

### Response translation

The gateway maps Anthropic response fields back to the OpenAI format before returning to the client:

| Anthropic field | OpenAI field | Notes |
|---|---|---|
| `id` | `id` | |
| `content[].text` | `choices[0].message.content` | Text blocks are concatenated |
| `usage.input_tokens` | `usage.prompt_tokens` | |
| `usage.output_tokens` | `usage.completion_tokens` | |
| `stop_reason` | `choices[0].finish_reason` | `end_turn`/`stop_sequence` → `stop`; `max_tokens` → `length` |

### Streaming translation

Anthropic uses a different streaming event format than OpenAI. The gateway translates on the fly:

| Anthropic event | Action |
|---|---|
| `message_start` | Captures the message ID for subsequent chunks |
| `content_block_delta` | Emitted as an OpenAI-format `chat.completion.chunk` with the delta text |
| `message_delta` | Emitted as a chunk with the `finish_reason` |
| `message_stop` | Emitted as the `[DONE]` sentinel |

### Model listing

Anthropic doesn't have a public models listing endpoint. The provider returns a static list of current
and legacy Claude models.

### Error translation

Anthropic error types are mapped to their gateway equivalents:

| Anthropic error type | Gateway error type | HTTP status |
|---|---|---|
| `authentication_error` | `authentication_error` | 401 |
| `rate_limit_error` | `rate_limit_error` | 429 |
| `invalid_request_error` | `invalid_request_error` | 400 |
| `not_found_error` | `not_found_error` | 404 |
| (other) | `server_error` | 500 |

## Local / self-hosted provider

The local provider is a direct pass-through to any OpenAI-compatible backend. Chat completions go to
`POST {base_url}/v1/chat/completions`. This means Ollama, vLLM, llama.cpp, SGLang, or any server
exposing this endpoint can be used.

Model listing tries `GET {base_url}/v1/models` first, falling back to Ollama's native
`GET {base_url}/api/tags`.

For multi-endpoint load balancing and failover, see [Multi-endpoint load balancing](multi-endpoint.md).

## Streaming

All three providers support streaming via Server-Sent Events (SSE). See [Streaming](streaming.md) for
response format, headers, and how the gateway processes streaming requests.

## Error handling

All providers translate upstream errors into a consistent OpenAI-compatible format:

```json
{
  "error": {
    "message": "description of what went wrong",
    "type": "error_type",
    "param": null,
    "code": null
  }
}
```

| Error type | HTTP status | Typical cause |
|---|---|---|
| `invalid_request_error` | 400 | Malformed request, missing model, provider not configured |
| `authentication_error` | 401 | Invalid API key |
| `permission_error` | 403 | Insufficient permissions |
| `not_found_error` | 404 | Model not found |
| `rate_limit_error` | 429 | Upstream rate limit hit |
| `server_error` | 500 | Provider returned an unexpected error |
| `service_unavailable` | 503 | Provider is down |
