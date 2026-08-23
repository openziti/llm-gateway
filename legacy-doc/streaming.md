# Streaming

The gateway supports streaming chat completions using Server-Sent Events (SSE). Clients send `"stream": true` in the request and receive response tokens incrementally as they are generated.

## Client Usage

Send a normal chat completion request with `stream` set to `true`:

```bash
curl -N http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama3",
    "stream": true,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

The response is a series of SSE events:

```
data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","model":"llama3","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","model":"llama3","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","model":"llama3","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","model":"llama3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

Each event is a `data:` line followed by a blank line. The stream ends with `data: [DONE]`.

## How It Works

### Request Flow

1. The client sends `POST /v1/chat/completions` with `"stream": true`.
2. The gateway routes the request to the appropriate provider.
3. The provider opens a streaming connection to the upstream API and returns a Go channel (`<-chan StreamEvent`).
4. The gateway sets SSE response headers and begins writing chunks as they arrive on the channel.
5. Each chunk is JSON-marshaled and written as a `data:` event, then flushed immediately.
6. When the provider signals completion, the gateway writes `data: [DONE]` and closes the connection.

### Response Headers

The gateway sets these headers before streaming begins:

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

The `X-Accel-Buffering: no` header tells nginx (and compatible reverse proxies) not to buffer the response. Without this, a reverse proxy might wait for the entire response before forwarding any data, defeating the purpose of streaming.

### Error Handling

If an error occurs **before** streaming starts (e.g., invalid request, provider unreachable), the gateway returns a normal JSON error response.

If an error occurs **during** streaming (e.g., upstream connection drops), the error is sent as an SSE event containing an error JSON object, and the stream is closed.

## Provider Differences

All three providers support streaming, but the underlying protocols differ. The gateway normalizes them into a consistent OpenAI-format SSE stream.

### OpenAI and Ollama

Both natively produce OpenAI-format SSE streams. The gateway reads `data:` lines, parses each as a `StreamChunk`, and forwards them directly. The `[DONE]` sentinel terminates the stream.

### Anthropic

Anthropic uses a different event format. The gateway translates on the fly:

| Anthropic event | Gateway action |
|---|---|
| `message_start` | captures the message ID for use in subsequent chunks |
| `content_block_delta` | emitted as an OpenAI-format chunk with delta content |
| `message_delta` | emitted as a chunk with the `finish_reason` translated (`end_turn` becomes `stop`, `max_tokens` becomes `length`) |
| `message_stop` | emitted as the `[DONE]` sentinel |

Other Anthropic events (`content_block_start`, `content_block_stop`, `ping`) are silently skipped.

## Chunk Format

Each streaming chunk follows the OpenAI format:

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion.chunk",
  "created": 1700000000,
  "model": "llama3",
  "choices": [
    {
      "index": 0,
      "delta": {
        "content": "token text"
      },
      "finish_reason": null
    }
  ]
}
```

The first chunk typically includes `"delta": {"role": "assistant"}` to signal the start of the assistant's message. Subsequent chunks contain `"delta": {"content": "..."}` with the generated text. The final chunk includes `"finish_reason": "stop"` (or `"length"` if truncated).

The `delta` field contains only the incremental content, not the full message. Clients accumulate deltas to reconstruct the complete response.
