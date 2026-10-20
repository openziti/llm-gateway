---
title: Semantic routing
sidebar_label: Semantic routing
---

# Semantic routing

When a request arrives without a `model` field (or with `model: auto`), the gateway uses semantic
routing to decide which backend model should handle it. Routing uses a three-layer cascade: fast
heuristic rules are tried first, then embedding-based similarity, then an LLM classifier. Each layer
can either make a confident decision or pass to the next. If no layer produces a match, the request
falls back to a configured default route.

## The routing cascade

The router evaluates layers in order and stops at the first confident result:

```
Request arrives
    |
    v
1. Explicit model? ──yes──> use that model (bypass routing)
    │no
    v
2. Heuristics match? ──yes──> use matched route
    │no
    v
3. Embeddings match?
    ├─ confident (>= threshold) ──> use matched route
    ├─ ambiguous (>= ambiguous_threshold but < threshold) ──> escalate to classifier
    └─ no match
    v
4. Classifier match? ──yes──> use classified route
    │no
    v
5. Default route
```

Each step appends to a **cascade log** visible in the gateway's output:

```
semantic routing: method=semantic route='coding' model='claude-haiku-4-5-20251001'
  confidence=0.87 latency=12ms cascade=[heuristic:no_match,semantic:coding:0.87]
```

### Explicit model passthrough

If the client sends a `model` field and `allow_explicit_model` is `true` (the default), the router uses
that model directly without evaluating any layers:

```yaml
routing:
  allow_explicit_model: true  # default; set false to force all requests through routing
```

### The `auto` virtual model

Clients that always require a `model` field (such as Open WebUI) can send `model: auto`. The gateway
clears this to an empty string before routing, which triggers the full cascade. When semantic routing
is enabled, `auto` appears in the `/v1/models` endpoint so clients can discover it.

## Routes

A route maps a name to a backend model and provides context for the embedding and classifier layers:

```yaml
routes:
  - name: coding
    model: claude-haiku-4-5-20251001
    description: "code generation, debugging, code review, and technical programming tasks"
    examples:
      - "write a python function to sort a list"
      - "debug this segfault in my C code"
      - "review this pull request for bugs"
      - "implement a binary search tree in Go"
```

Each field serves a specific role across the routing layers:

| Field | Used by | Purpose |
|---|---|---|
| `name` | All layers | Identifier for heuristic rules, cascade logs, and classifier output |
| `model` | All layers | The backend model to use when this route is selected |
| `description` | Classifier | Included in the classifier prompt |
| `examples` | Embeddings | Converted to vectors at startup for similarity matching |

## Layer 1: Heuristics

Heuristics are fast, deterministic rules evaluated before any model calls:

```yaml
heuristics:
  enabled: true
  rules:
    - match:
        keywords: ["translate", "translation"]
      route: general
    - match:
        has_tools: true
      route: tools
    - match:
        system_prompt_contains: "you are a code assistant"
      route: coding
    - match:
        max_tokens_lt: 100
        message_length_lt: 200
      route: fast
```

Rules are evaluated in order. The first matching rule wins. All conditions within a rule must be true
(AND logic).

### Match conditions

These conditions can appear in a match block:

- **`keywords`**: Matched against user messages with word boundaries, case-insensitive. Any single
  keyword matching is sufficient.
- **`exclude`**: Phrases that suppress a keyword match. If any exclusion phrase is found, the rule
  doesn't match. Useful for filtering out boilerplate text injected by clients like Open WebUI.
- **`system_prompt_contains`**: A substring matched against any system message, case-insensitive.
- **`max_tokens_lt`**: Matches if `max_tokens` is set and strictly less than the given value.
- **`message_length_lt`**: Matches if the total character count across all messages is strictly less
  than the given value.
- **`has_tools`**: Matches if the request includes tool definitions (`true`) or does not (`false`).

### Exclusions

When using broad keywords, you may encounter false positives from boilerplate text injected by clients:

```yaml
- match:
    keywords: ["code", "debug", "refactor"]
    exclude: ["code fences", "code block", "### Task"]
  route: coding
```

Exclusions are checked first. If any exclusion phrase matches, the rule is skipped entirely.

## Layer 2: Embeddings

The embedding layer converts text into numerical vectors and uses cosine similarity to find the closest
route.

At startup, each route's example prompts are embedded and stored in memory. When a request arrives, the
last user message is embedded and compared against each route's stored vectors. Messages longer than
2048 characters are truncated before embedding.

### Configuration

Set the following options under `semantic:` in your routing config:

```yaml
semantic:
  enabled: true
  provider: local           # local or openai
  model: nomic-embed-text   # embedding model name
  threshold: 0.75           # minimum similarity for a confident match
  ambiguous_threshold: 0.5  # below threshold but above this → escalate to classifier
  comparison: centroid      # centroid, max, or average
  cache_embeddings: true    # cache prompt embeddings to avoid repeated calls
  cache_ttl: 3600           # cache entry lifetime in seconds (default: 3600)
  cache_size: 1000          # maximum cache entries (default: 1000)
```

### Comparison modes

Three modes control how the embedding layer compares a request against stored route examples:

- **`centroid`** (default): Averages all example embeddings into a single vector per route. Fastest.
  Works well when examples cluster around a common theme.
- **`max`**: Compares against every example individually and uses the highest score. Good when a route
  covers several distinct sub-topics. More prone to false positives.
- **`average`**: Compares against every example individually and uses the mean score. Balanced between
  `centroid` and `max`.

| Situation | Recommended mode |
|---|---|
| Examples per route are similar to each other | `centroid` |
| A route covers several distinct sub-topics | `max` |
| You want balanced "generally like this route" scoring | `average` |

### Thresholds

```
score >= threshold                            → confident match, return immediately
ambiguous_threshold <= score < threshold      → ambiguous, escalate to classifier
score < ambiguous_threshold                   → no match, continue to next layer
```

The right values depend on your embedding model and route structure. Models like `nomic-embed-text`
tend to produce higher similarity scores, so you may need higher thresholds (0.7–0.85 for `threshold`,
0.4–0.6 for `ambiguous_threshold`).

### Embedding cache

When `cache_embeddings` is true, prompt embeddings are cached in an LRU cache keyed by a SHA-256 hash
of the prompt text. `cache_size` controls capacity (evicts least recently used when full).

## Layer 3: LLM classifier

The classifier sends the user's prompt to a chat model and asks it to classify the request into one of
the configured routes. It's typically used as a fallback for ambiguous embedding results but can also
run standalone.

### Configuration

Set the following options under `classifier:` in your routing config:

```yaml
classifier:
  enabled: true
  provider: local            # local or openai
  model: qwen3-vl:30b
  timeout_ms: 10000          # request timeout in milliseconds (0 = no timeout)
  confidence_threshold: 0.7  # minimum confidence to accept the classification
  cache_results: true
  cache_ttl: 3600
  cache_size: 500
```

### When the classifier runs

The classifier is invoked when:

- The embedding layer found a route but the score was between `ambiguous_threshold` and `threshold`.
- Embeddings are disabled and heuristics found no match.

The classifier's result is accepted only if the confidence meets or exceeds `confidence_threshold`.

### Route descriptions matter

The classifier relies on the `description` field to understand what each route represents. Write
descriptions that are specific enough for an LLM to distinguish between routes — vague descriptions
produce poor classifications.

The classifier's response may be wrapped in markdown code blocks. The gateway strips those
automatically before parsing the result.

## Default route

If no layer produces a confident result, the gateway uses:

```yaml
routing:
  default_route: general
```

If `default_route` isn't set, the first route in the `routes` list is the absolute fallback.

## Example configuration

A minimal setup using only the embedding layer, with two routes:

```yaml
routing:
  default_route: general

  semantic:
    enabled: true
    provider: local
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5

  routes:
    - name: coding
      model: claude-haiku-4-5-20251001
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
        - "debug this segfault in my C code"

    - name: general
      model: qwen3-vl:30b
      description: "general knowledge and conversation"
      examples:
        - "what is the capital of France"
        - "explain how photosynthesis works"
```

## Full configuration reference

All routing options in a single block with defaults shown:

```yaml
routing:
  allow_explicit_model: true
  default_route: general

  heuristics:
    enabled: true
    rules:
      - match:
          keywords: [...]
          exclude: [...]
          system_prompt_contains: "..."
          max_tokens_lt: 100
          message_length_lt: 200
          has_tools: true
        route: route_name

  semantic:
    enabled: true
    provider: local
    model: nomic-embed-text
    threshold: 0.75
    ambiguous_threshold: 0.5
    comparison: centroid
    cache_embeddings: false   # default: false
    cache_ttl: 3600
    cache_size: 1000

  classifier:
    enabled: true
    provider: local
    model: qwen3-vl:30b
    timeout_ms: 0             # default: 0 (no timeout)
    confidence_threshold: 0   # default: 0
    cache_results: false      # default: false
    cache_ttl: 3600
    cache_size: 500

  routes:
    - name: coding
      model: claude-haiku-4-5-20251001
      description: "code generation, debugging, and technical tasks"
      examples:
        - "write a python function to sort a list"
        - "debug this segfault in my C code"
```

## Tuning tips

A few principles for getting good routing results:

- **Start simple.** Enable only the embedding layer with a few well-chosen examples per route. Add
  heuristics and the classifier later if needed.
- **Add more examples before switching comparison modes.** Four well-chosen examples often solve
  problems that changing `comparison` won't.
- **Keep examples realistic.** Use prompts that look like what users actually send.
- **Use heuristics for obvious cases.** If every request containing "translate" should go to the same
  route, a keyword heuristic is faster and more reliable than embedding similarity.
- **Watch the cascade logs.** The gateway logs the full cascade for every routed request. This is the
  best way to understand why a request was routed where it was.
- **Use metrics for aggregate tuning.** A high proportion of `default` decisions suggests your
  thresholds are too strict or your examples don't cover your traffic well.
