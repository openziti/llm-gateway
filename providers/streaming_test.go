package providers

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestChoiceFinishReasonJSONShape(t *testing.T) {
	// intermediate chunk: nil FinishReason must serialize as null, not vanish.
	b, _ := json.Marshal(Choice{Index: 0, Delta: &Delta{}})
	if !strings.Contains(string(b), `"finish_reason":null`) {
		t.Errorf("intermediate choice = %s, want finish_reason:null", b)
	}

	// terminal chunk: populated FinishReason serializes as its value.
	fr := "stop"
	b, _ = json.Marshal(Choice{Index: 0, Delta: &Delta{}, FinishReason: &fr})
	if !strings.Contains(string(b), `"finish_reason":"stop"`) {
		t.Errorf("terminal choice = %s, want finish_reason:stop", b)
	}
}

func TestParseStreamChunkErrorEnvelope(t *testing.T) {
	// an error envelope must surface as an error, not an all-zero chunk.
	_, err := parseStreamChunk(`{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`)
	if err == nil {
		t.Fatal("error envelope must return an error")
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Errorf("err = %q, want the upstream message", err)
	}

	// a normal chunk parses as a chunk.
	chunk, err := parseStreamChunk(`{"id":"chatcmpl-1","object":"chat.completion.chunk"}`)
	if err != nil {
		t.Fatalf("normal chunk parse = %v, want nil", err)
	}
	if chunk == nil || chunk.ID != "chatcmpl-1" {
		t.Errorf("chunk = %+v, want id chatcmpl-1", chunk)
	}
}

func TestOpenAIStreamErrorEnvelopeSurfaces(t *testing.T) {
	o := NewOpenAI("test-key", "")

	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"partial"}}]}`,
		``,
		`data: {"error":{"message":"upstream overloaded","type":"server_error"}}`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go o.readSSEStream(io.NopCloser(strings.NewReader(sse)), events)

	var streamErr error
	var done bool
	for ev := range events {
		if ev.Err != nil {
			streamErr = ev.Err
		}
		if ev.Done {
			done = true
		}
	}

	if streamErr == nil {
		t.Fatal("mid-stream error envelope must surface as a stream error")
	}
	if !strings.Contains(streamErr.Error(), "upstream overloaded") {
		t.Errorf("stream error = %q, want the upstream message", streamErr)
	}
	if done {
		t.Error("an errored stream must not also emit Done")
	}
}

func TestOpenAIStreamEmitsTerminalUsage(t *testing.T) {
	o := NewOpenAI("test-key", "")

	// with stream_options.include_usage, OpenAI sends a terminal chunk whose
	// choices is empty and which carries usage.
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go o.readSSEStream(io.NopCloser(strings.NewReader(sse)), events)

	var usage *Usage
	var contentChunks int
	for ev := range events {
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if ev.Chunk != nil {
			contentChunks++
		}
	}

	if usage == nil {
		t.Fatal("terminal usage chunk must surface as a Usage event")
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want prompt=11 completion=7", usage)
	}
	// the empty-choices usage chunk must not also be forwarded as a content chunk.
	if contentChunks != 1 {
		t.Errorf("content chunks = %d, want 1 (usage chunk must not be forwarded)", contentChunks)
	}
}

func TestAnthropicStreamEmitsUsageBeforeDone(t *testing.T) {
	a := NewAnthropic("test-key", "")

	// input tokens arrive in message_start; output tokens accumulate (cumulative)
	// in message_delta; usage is emitted on message_stop, just before Done.
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":25,"output_tokens":1}}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go a.readSSEStream(io.NopCloser(strings.NewReader(sse)), events, "claude-test")

	var order []string
	var usage *Usage
	for ev := range events {
		switch {
		case ev.Usage != nil:
			usage = ev.Usage
			order = append(order, "usage")
		case ev.Done:
			order = append(order, "done")
		}
	}

	if usage == nil {
		t.Fatal("anthropic stream must emit a Usage event")
	}
	if usage.PromptTokens != 25 || usage.CompletionTokens != 9 || usage.TotalTokens != 34 {
		t.Errorf("usage = %+v, want prompt=25 completion=9 total=34", usage)
	}
	if len(order) != 2 || order[0] != "usage" || order[1] != "done" {
		t.Errorf("event order = %v, want [usage done]", order)
	}
}
