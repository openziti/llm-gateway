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

// TestOpenAIStreamUsagePassthrough shows the OpenAI provider needs no special handling:
// OpenAI natively emits the include_usage final chunk (empty choices + usage), and the
// gateway now preserves it because StreamChunk carries a usage field. This is pass-through,
// not translation (unlike anthropic, which has no stream_options and is synthesized).
func TestOpenAIStreamUsagePassthrough(t *testing.T) {
	o := NewOpenAI("test-key", "")

	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go o.readSSEStream(io.NopCloser(strings.NewReader(sse)), events)

	var last *StreamChunk
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
		if ev.Chunk != nil {
			last = ev.Chunk
		}
	}
	if last == nil || last.Usage == nil {
		t.Fatal("OpenAI usage chunk was not preserved through the gateway")
	}
	if last.Usage.PromptTokens != 10 || last.Usage.CompletionTokens != 5 || last.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v, want prompt=10 completion=5 total=15", last.Usage)
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
