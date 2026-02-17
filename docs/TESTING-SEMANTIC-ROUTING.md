# Testing Semantic Routing

Manual test guide for verifying semantic routing end-to-end.

## Prerequisites

1. **Ollama** running locally with models pulled:
   ```bash
   ollama pull llama3.2
   ollama pull nomic-embed-text
   ```

2. **Anthropic API key** set in environment:
   ```bash
   export ANTHROPIC_API_KEY="sk-ant-..."
   ```

3. **Gateway built:**
   ```bash
   go install ./...
   ```

## Setup

Start the gateway with the test config:

```bash
llm-gateway run -c etc/test-semantic-routing.yaml
```

To use with **Open WebUI** or any OpenAI-compatible client, point it at `http://localhost:8080/v1` and select the `auto` model from the model list.

To verify the `auto` model appears:

```bash
curl -s http://localhost:8080/v1/models | jq '.data[] | select(.id == "auto")'
```

## Test Prompts

Send each prompt and check the gateway log output for the routing decision.

### 1. Coding task (semantic match)

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Write a Python function that reverses a linked list"}]}'
```

**Expected:** route=`coding`, model=`claude-sonnet-4-20250514`, method=`semantic`

### 2. Translation (heuristic match)

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Translate '\''good morning'\'' to Japanese"}]}'
```

**Expected:** route=`general`, method=`heuristic` (matches "translate" keyword)

### 3. General knowledge (default/semantic)

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "What'\''s the weather like on Mars?"}]}'
```

**Expected:** route=`general`, model=`llama3.2`, method=`semantic` or `default`

### 4. Code review (semantic match)

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "auto", "messages": [{"role": "user", "content": "Review this code: def foo(): pass"}]}'
```

**Expected:** route=`coding`, model=`claude-sonnet-4-20250514`, method=`semantic`

### 5. Explicit model (bypass routing)

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "llama3.2", "messages": [{"role": "user", "content": "Hello, how are you?"}]}'
```

**Expected:** no semantic routing log line; model routed directly to Ollama.

## What to Look For

After each request, the gateway logs a line like:

```
semantic routing: method=semantic route='coding' model='claude-sonnet-4-20250514' confidence=0.85 latency=42ms cascade=[heuristic,semantic]
```

- **method** shows which layer made the decision
- **route** is the matched route name
- **model** is the model that will handle the request
- **confidence** is the match confidence (1.0 for heuristic/explicit, variable for semantic/classifier)
- **cascade** shows which layers were attempted

## Verification

- Coding prompts (1, 4) should produce Anthropic-style responses (check the `model` field in the JSON response for a Claude model name).
- General prompts (2, 3) should produce Ollama-style responses (check the `model` field for `llama3.2`).
- Explicit model (5) should bypass semantic routing entirely with no routing log line.
