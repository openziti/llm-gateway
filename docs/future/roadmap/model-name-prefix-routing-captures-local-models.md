---
title: model name prefix routing captures local models
state: inbox
created: 2026-11-24
tags: [defect]
---

A local model whose name begins with `gpt-`, `o1-`, or `o3-` cannot be reached. `resolveProvider` picks the provider from the name prefix alone, so `gpt-oss:20b` served by Ollama routes to OpenAI — a 400 when OpenAI is not configured, a request to OpenAI for a model it does not serve when it is. `claude-` behaves the same way toward Anthropic.

Decide how a model the local backend actually serves takes precedence over the prefix table: consult what local advertises before applying prefixes, give the operator an explicit per-model override, or both.

## Discussion

`/v1/models` lists the model, because listing asks the local backend directly, so the gateway advertises an id it then refuses to route. Nothing in the config can redirect it; the prefix table is the only input to provider selection, for explicit models, semantic-route models, and capability aliases alike.

The 400 also hides the cause. `Route` returns an empty provider type on its error path and the handler formats the client message from that value, so the client sees `provider '' is not configured` where the docs promise the provider's name. Read from the code, not yet reproduced against a running gateway.
