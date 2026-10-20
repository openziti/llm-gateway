---
title: Streaming
sidebar_label: Streaming
---

# Streaming

All providers support streaming chat completions via Server-Sent Events (SSE).

## How the gateway handles streaming

When the client sends `"stream": true`, the gateway:

1. Sends the request to the upstream provider with streaming enabled.
2. Sets SSE response headers (`Content-Type: text/event-stream`, `Cache-Control: no-cache`,
   `X-Accel-Buffering: no`).
3. Reads chunks from the provider as they arrive.
4. Writes each chunk as a `data: {json}\n\n` SSE event and flushes immediately.
5. Sends `data: [DONE]\n\n` when the stream completes.

## Send a streaming request

### curl

Include `"stream": true` in your request to receive incremental token output:

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Explain quantum entanglement"}],
    "stream": true
  }'
```

### Python

Use the OpenAI Python client with `stream=True`:

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="not-needed")

stream = client.chat.completions.create(
    model="claude-sonnet-4-20250514",
    messages=[{"role": "user", "content": "Write a haiku"}],
    stream=True,
)

for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="")
```

## Response format

The gateway returns a series of SSE events. Each chunk follows the OpenAI format:

```
data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"delta":{"content":"Quantum"},"index":0}]}

data: {"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"delta":{"content":" entanglement"},"index":0}]}

data: [DONE]
```

Each `delta` field contains only the incremental content for that chunk. Clients must accumulate chunks
to reconstruct the full message.

## Response headers

The gateway sets these headers on streaming responses:

| Header | Value | Purpose |
|---|---|---|
| `Content-Type` | `text/event-stream` | identifies the response as SSE |
| `Cache-Control` | `no-cache` | prevents caching of the stream |
| `Connection` | `keep-alive` | keeps the connection open |
| `X-Accel-Buffering` | `no` | disables nginx buffering so chunks reach clients immediately |

## Provider differences

**OpenAI and local (Ollama, vLLM, etc.)** — these already produce OpenAI-format SSE streams. The gateway
forwards them directly to the client.

**Anthropic** — uses a different event protocol. The gateway translates on the fly:

| Anthropic event | Gateway action |
|---|---|
| `message_start` | captures message ID for subsequent chunks |
| `content_block_delta` | emitted as an OpenAI-format `chat.completion.chunk` |
| `message_delta` | emitted as a chunk with `finish_reason` (`end_turn` → `stop`) |
| `message_stop` | emitted as `data: [DONE]` |

Translation is transparent — clients receive the same format regardless of which provider handled the
request.

## Error handling

If an error occurs before streaming starts, the gateway returns a standard JSON error response. If an
error occurs mid-stream (after the SSE connection is established), it's sent as an SSE event containing
an error JSON object before the connection closes.
