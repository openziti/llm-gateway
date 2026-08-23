---
title: NetFoundry LLM Gateway overview
sidebar_label: Overview
description: >
  NetFoundry LLM Gateway is an OpenAI-compatible API proxy that routes requests across multiple LLM
  providers using zero-trust networking over OpenZiti.
---

# NetFoundry LLM Gateway overview

NetFoundry LLM Gateway is an OpenAI-compatible API proxy that routes requests across multiple LLM
providers using zero-trust networking. The project is open source and can be found at:
[github.com/openziti/llm-gateway](https://github.com/openziti/llm-gateway).

## What it does

It handles provider routing, format translation, and zero-trust networking so clients interact with a
single OpenAI-compatible endpoint regardless of which model or provider handles the request.

- **Multi-provider routing**: Routes requests to OpenAI, Anthropic, and any OpenAI-compatible backend
  (Ollama, vLLM, llama-server, SGLang, etc.) by prefix-matching on the model name.
- **Zero-trust networking**: Uses zrok over OpenZiti overlay networks to connect to backends across NAT
  and air-gapped environments — no firewall configuration needed.
- **Semantic routing**: A three-layer cascade (keyword heuristics → embedding similarity → LLM classifier)
  automatically selects the right model when the client omits a model name.
- **Load balancing**: Weighted round-robin across multiple inference servers with health checks and passive
  failover.
- **Single binary**: One Go binary, one YAML config file — no database, message queue, or sidecar.

## API endpoints

The gateway exposes standard OpenAI-compatible endpoints:

| Endpoint | Description |
|---|---|
| `POST /v1/chat/completions` | Chat completions (streaming and non-streaming) |
| `GET /v1/models` | List available models from all providers |
| `GET /health` | Health check |
| `GET /metrics` | Prometheus metrics (when enabled) |

Streaming works via Server-Sent Events across all providers. Anthropic requests are automatically
translated to and from OpenAI format, so existing tools that speak OpenAI work without changes.

## Observability

Prometheus metrics track request volume, latency, token usage, routing decisions, and endpoint health.
Per-request body logging is available for debugging routing behavior.

