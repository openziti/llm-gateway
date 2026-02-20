# Providers

The gateway presents a single OpenAI-compatible API to clients and translates requests to the appropriate backend provider. Three providers are supported: OpenAI, Anthropic, and Ollama.

## API Surface

All clients interact with the gateway using the OpenAI chat completions format:

```
POST /v1/chat/completions    chat completions (streaming and non-streaming)
GET  /v1/models              list available models from all providers
GET  /health                 health check
GET  /metrics                Prometheus metrics (when enabled)
```

Requests and responses follow the [OpenAI API format](https://platform.openai.com/docs/api-reference/chat). The gateway does not require an API key from clients -- authentication is between the gateway and the upstream providers.

## Model Routing

Models are routed to providers by prefix matching on the model name:

| Prefix | Provider |
|---|---|
| `gpt-*`, `o1-*`, `o3-*` | OpenAI |
| `claude-*` | Anthropic |
| everything else | Ollama |

Matching is case-insensitive. A request for `gpt-4` goes to OpenAI; a request for `claude-haiku-4-5-20251001` goes to Anthropic; a request for `llama3` or `qwen3-vl:30b` goes to Ollama.

If the target provider isn't configured, the gateway returns an error:

```json
{"error": {"message": "provider 'openai' is not configured", "type": "invalid_request_error"}}
```

## OpenAI Provider

The OpenAI provider is a direct pass-through. Requests are forwarded to `POST {base_url}/v1/chat/completions` with an `Authorization: Bearer` header. Responses are returned unmodified.

This means any OpenAI-compatible API can be used as the OpenAI provider by setting `base_url` -- for example, Azure OpenAI or a local vLLM server.

Model listing calls `GET {base_url}/v1/models`.

## Anthropic Provider

The Anthropic provider translates between the OpenAI format and [Anthropic's Messages API](https://docs.anthropic.com/en/docs/api-reference/messages/create). This translation is transparent to clients -- they send OpenAI-format requests and receive OpenAI-format responses regardless of which provider handles the request.

### Request Translation

| OpenAI field | Anthropic field | Notes |
|---|---|---|
| `model` | `model` | passed through |
| `messages` (role: system) | `system` | first system message extracted into Anthropic's top-level `system` field |
| `messages` (role: user) | `messages` (role: user) | |
| `messages` (role: assistant) | `messages` (role: assistant) | |
| `messages` (role: tool) | `messages` (role: user) | mapped to user role |
| `max_tokens` | `max_tokens` | defaults to 4096 if not set (Anthropic requires this field) |
| `temperature` | `temperature` | |
| `top_p` | `top_p` | |
| `stop` | `stop_sequences` | string or array |

Requests are sent to `POST {base_url}/v1/messages` with headers:
- `x-api-key: {api_key}`
- `anthropic-version: 2023-06-01`
- `Content-Type: application/json`

### Response Translation

| Anthropic field | OpenAI field | Notes |
|---|---|---|
| `id` | `id` | |
| `content[].text` | `choices[0].message.content` | text blocks are concatenated |
| `usage.input_tokens` | `usage.prompt_tokens` | |
| `usage.output_tokens` | `usage.completion_tokens` | |
| `stop_reason` | `choices[0].finish_reason` | `end_turn` and `stop_sequence` become `stop`; `max_tokens` becomes `length` |

### Streaming Translation

Anthropic uses a different streaming event format than OpenAI. The gateway translates on the fly:

| Anthropic event | Action |
|---|---|
| `message_start` | captures the message ID for subsequent chunks |
| `content_block_delta` | emitted as an OpenAI-format `chat.completion.chunk` with the delta text |
| `message_delta` | emitted as a chunk with the `finish_reason` |
| `message_stop` | emitted as the `[DONE]` sentinel |

### Model Listing

Anthropic does not have a public models listing endpoint. The provider returns a static list of current and legacy Claude models.

### Error Translation

Anthropic error types are mapped to the gateway's OpenAI-compatible error types:

| Anthropic error type | Gateway error type | HTTP status |
|---|---|---|
| `authentication_error` | `authentication_error` | 401 |
| `rate_limit_error` | `rate_limit_error` | 429 |
| `invalid_request_error` | `invalid_request_error` | 400 |
| `not_found_error` | `not_found_error` | 404 |
| (other) | `server_error` | 500 |

## Ollama Provider

Ollama natively implements an [OpenAI-compatible API](https://github.com/ollama/ollama/blob/main/docs/openai.md), so the Ollama provider is a near-direct pass-through. Chat completions go to `POST {base_url}/v1/chat/completions`.

Model listing calls Ollama's native `GET {base_url}/api/tags` endpoint and translates the response into the OpenAI models format.

For multi-endpoint Ollama with load balancing and failover, see [docs/ollama-multi-endpoint.md](ollama-multi-endpoint.md).

## Streaming

All three providers support streaming via Server-Sent Events (SSE). When the client sends `"stream": true`, the gateway:

1. Sends the request to the upstream provider with streaming enabled
2. Sets SSE response headers:
   - `Content-Type: text/event-stream`
   - `Cache-Control: no-cache`
   - `Connection: keep-alive`
   - `X-Accel-Buffering: no` (disables nginx buffering)
3. Reads chunks from the provider as they arrive
4. Writes each chunk as a `data: {json}\n\n` SSE event, flushing after each one
5. Sends `data: [DONE]\n\n` when the stream completes

If an error occurs mid-stream, it is sent as an SSE event containing an error JSON object.

## Error Handling

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

Error types and their HTTP status codes:

| Error type | HTTP status | Typical cause |
|---|---|---|
| `invalid_request_error` | 400 | malformed request, missing model, provider not configured |
| `authentication_error` | 401 | invalid API key |
| `permission_error` | 403 | insufficient permissions |
| `not_found_error` | 404 | model not found |
| `rate_limit_error` | 429 | upstream rate limit hit |
| `server_error` | 500 | provider returned an unexpected error |
| `service_unavailable` | 503 | provider is down |

The gateway first tries to parse the upstream error response into this format. If that fails, it falls back to a generic error based on the HTTP status code.
