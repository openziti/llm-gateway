package providers

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAnthropicTranslateRequest(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tests := []struct {
		name     string
		req      *ChatCompletionRequest
		wantSys  string
		wantMsgs int
		wantMax  int
	}{
		{
			name: "basic request without system",
			req: &ChatCompletionRequest{
				Model: "claude-3-opus",
				Messages: []Message{
					{Role: "user", Content: "Hello"},
				},
			},
			wantSys:  "",
			wantMsgs: 1,
			wantMax:  4096, // default
		},
		{
			name: "request with system message",
			req: &ChatCompletionRequest{
				Model: "claude-3-opus",
				Messages: []Message{
					{Role: "system", Content: "You are helpful"},
					{Role: "user", Content: "Hello"},
				},
			},
			wantSys:  "You are helpful",
			wantMsgs: 1, // system extracted, only user message remains
			wantMax:  4096,
		},
		{
			name: "request with max_tokens",
			req: &ChatCompletionRequest{
				Model:     "claude-3-opus",
				MaxTokens: intPtr(1000),
				Messages: []Message{
					{Role: "user", Content: "Hello"},
				},
			},
			wantSys:  "",
			wantMsgs: 1,
			wantMax:  1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.translateRequest(tt.req)

			if result.System != tt.wantSys {
				t.Errorf("System = %q, want %q", result.System, tt.wantSys)
			}
			if len(result.Messages) != tt.wantMsgs {
				t.Errorf("Messages count = %d, want %d", len(result.Messages), tt.wantMsgs)
			}
			if result.MaxTokens != tt.wantMax {
				t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, tt.wantMax)
			}
		})
	}
}

func TestAnthropicTranslateStopReason(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tests := []struct {
		input string
		want  string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"stop_sequence", "stop"},
		{"tool_use", "tool_calls"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := a.translateStopReason(tt.input)
			if got != tt.want {
				t.Errorf("translateStopReason(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAnthropicExtractContent(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tests := []struct {
		name    string
		content any
		want    string
	}{
		{
			name:    "string content",
			content: "Hello world",
			want:    "Hello world",
		},
		{
			name: "array content with text",
			content: []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello"},
				map[string]interface{}{"type": "text", "text": "World"},
			},
			want: "Hello\nWorld",
		},
		{
			name:    "nil content",
			content: nil,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.extractContent(tt.content)
			if got != tt.want {
				t.Errorf("extractContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnthropicTranslateResponse(t *testing.T) {
	a := NewAnthropic("test-key", "")

	resp := &anthropicResponse{
		ID:   "msg_123",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContentBlock{
			{Type: "text", Text: "Hello "},
			{Type: "text", Text: "world!"},
		},
		Model:      "claude-3-opus-20240229",
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	result := a.translateResponse(resp, "claude-3-opus-20240229")

	if result.ID != "msg_123" {
		t.Errorf("ID = %q, want %q", result.ID, "msg_123")
	}
	if result.Object != "chat.completion" {
		t.Errorf("Object = %q, want %q", result.Object, "chat.completion")
	}
	if len(result.Choices) != 1 {
		t.Fatalf("Choices count = %d, want 1", len(result.Choices))
	}

	choice := result.Choices[0]
	if choice.FinishReason == nil || *choice.FinishReason != "stop" {
		t.Errorf("FinishReason = %v, want %q", choice.FinishReason, "stop")
	}
	if choice.Message == nil {
		t.Fatal("Message is nil")
	}
	if choice.Message.Role != "assistant" {
		t.Errorf("Message.Role = %q, want %q", choice.Message.Role, "assistant")
	}

	// content should be joined
	content, ok := choice.Message.Content.(string)
	if !ok {
		t.Fatalf("Message.Content is not string: %T", choice.Message.Content)
	}
	if content != "Hello world!" {
		t.Errorf("Message.Content = %q, want %q", content, "Hello world!")
	}

	if result.Usage == nil {
		t.Fatal("Usage is nil")
	}
	if result.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", result.Usage.PromptTokens)
	}
	if result.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", result.Usage.CompletionTokens)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestAnthropicRequestJSON(t *testing.T) {
	a := NewAnthropic("test-key", "")

	temp := 0.7
	req := &ChatCompletionRequest{
		Model:       "claude-3-opus",
		MaxTokens:   intPtr(2000),
		Temperature: &temp,
		Messages: []Message{
			{Role: "system", Content: "You are helpful"},
			{Role: "user", Content: "Hello"},
		},
		Stop: []interface{}{"END"},
	}

	result := a.translateRequest(req)

	// verify it marshals correctly
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed["model"] != "claude-3-opus" {
		t.Errorf("model = %v, want claude-3-opus", parsed["model"])
	}
	if parsed["max_tokens"].(float64) != 2000 {
		t.Errorf("max_tokens = %v, want 2000", parsed["max_tokens"])
	}
	if parsed["system"] != "You are helpful" {
		t.Errorf("system = %v, want 'You are helpful'", parsed["system"])
	}
}

func intPtr(i int) *int {
	return &i
}

func TestAnthropicTranslateTools(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tools := []Tool{
		{Type: "function", Function: Function{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
			},
		}},
		{Type: "function", Function: Function{Name: "no_params"}}, // nil params -> default schema
		{Type: "retrieval", Function: Function{Name: "skipme"}},   // non-function -> skipped
	}

	out := a.translateTools(tools)
	if len(out) != 2 {
		t.Fatalf("translateTools len = %d, want 2", len(out))
	}
	if out[0].Name != "get_weather" || out[0].Description != "Get weather" {
		t.Errorf("tool[0] = %+v", out[0])
	}
	// nil params default to an object schema
	schema, ok := out[1].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("tool[1].InputSchema type = %T, want map", out[1].InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf("tool[1] schema type = %v, want object", schema["type"])
	}
	if _, ok := schema["properties"]; !ok {
		t.Errorf("tool[1] schema missing properties")
	}
}

func TestAnthropicTranslateToolChoice(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tests := []struct {
		name     string
		input    any
		wantType string
		wantName string
		wantDrop bool
		wantNil  bool
	}{
		{"auto", "auto", "auto", "", false, false},
		{"required", "required", "any", "", false, false},
		{"none", "none", "", "", true, true},
		{"named", map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "get_weather"}}, "tool", "get_weather", false, false},
		{"unknown string", "wat", "", "", false, true},
		{"nil", nil, "", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			choice, drop := a.translateToolChoice(tt.input)
			if drop != tt.wantDrop {
				t.Errorf("dropTools = %v, want %v", drop, tt.wantDrop)
			}
			if tt.wantNil {
				if choice != nil {
					t.Errorf("choice = %+v, want nil", choice)
				}
				return
			}
			if choice == nil {
				t.Fatalf("choice = nil, want type %q", tt.wantType)
			}
			if choice.Type != tt.wantType {
				t.Errorf("choice.Type = %q, want %q", choice.Type, tt.wantType)
			}
			if choice.Name != tt.wantName {
				t.Errorf("choice.Name = %q, want %q", choice.Name, tt.wantName)
			}
		})
	}
}

func TestAnthropicTranslateRequestWithTools(t *testing.T) {
	a := NewAnthropic("test-key", "")

	tools := []Tool{{Type: "function", Function: Function{Name: "get_weather", Parameters: map[string]any{"type": "object"}}}}

	t.Run("auto", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model:      "claude-sonnet-4-6",
			Messages:   []Message{{Role: "user", Content: "weather?"}},
			Tools:      tools,
			ToolChoice: "auto",
		}
		ar := a.translateRequest(req)
		if len(ar.Tools) != 1 || ar.Tools[0].Name != "get_weather" {
			t.Fatalf("Tools = %+v", ar.Tools)
		}
		if ar.ToolChoice == nil || ar.ToolChoice.Type != "auto" {
			t.Fatalf("ToolChoice = %+v, want auto", ar.ToolChoice)
		}

		// verify the anthropic wire shape: tools[].input_schema and the
		// tool_choice object (the two translations the original bug dropped).
		data, err := json.Marshal(ar)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		toolsJSON, _ := parsed["tools"].([]any)
		if len(toolsJSON) != 1 {
			t.Fatalf("tools wire = %v", parsed["tools"])
		}
		tool0 := toolsJSON[0].(map[string]any)
		if _, ok := tool0["input_schema"]; !ok {
			t.Errorf("tool missing input_schema: %v", tool0)
		}
		tc, _ := parsed["tool_choice"].(map[string]any)
		if tc["type"] != "auto" {
			t.Errorf("tool_choice wire = %v, want {type:auto}", parsed["tool_choice"])
		}
	})

	t.Run("none drops tools", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model:      "claude-sonnet-4-6",
			Messages:   []Message{{Role: "user", Content: "weather?"}},
			Tools:      tools,
			ToolChoice: "none",
		}
		ar := a.translateRequest(req)
		if ar.Tools != nil {
			t.Errorf("Tools = %+v, want nil", ar.Tools)
		}
		if ar.ToolChoice != nil {
			t.Errorf("ToolChoice = %+v, want nil", ar.ToolChoice)
		}
	})
}

func TestAnthropicConvertAssistantToolCalls(t *testing.T) {
	a := NewAnthropic("test-key", "")

	t.Run("tool calls only", func(t *testing.T) {
		msg := Message{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function", Function: FunctionCall{Name: "get_weather", Arguments: `{"city":"NYC"}`}},
			},
		}
		am := a.convertAssistantMessage(msg)
		blocks, ok := am.Content.([]anthropicContentBlock)
		if !ok {
			t.Fatalf("Content type = %T, want []anthropicContentBlock", am.Content)
		}
		if len(blocks) != 1 {
			t.Fatalf("blocks = %d, want 1", len(blocks))
		}
		b := blocks[0]
		if b.Type != "tool_use" || b.ID != "call_1" || b.Name != "get_weather" {
			t.Errorf("block = %+v", b)
		}
		var input map[string]any
		if err := json.Unmarshal(b.Input, &input); err != nil {
			t.Fatalf("input not valid json: %v", err)
		}
		if input["city"] != "NYC" {
			t.Errorf("input city = %v, want NYC", input["city"])
		}
	})

	t.Run("mixed text and tool", func(t *testing.T) {
		msg := Message{
			Role:    "assistant",
			Content: "let me check",
			ToolCalls: []ToolCall{
				{ID: "call_1", Type: "function", Function: FunctionCall{Name: "get_weather", Arguments: `{}`}},
			},
		}
		am := a.convertAssistantMessage(msg)
		blocks := am.Content.([]anthropicContentBlock)
		if len(blocks) != 2 {
			t.Fatalf("blocks = %d, want 2", len(blocks))
		}
		if blocks[0].Type != "text" || blocks[0].Text != "let me check" {
			t.Errorf("block[0] = %+v, want text", blocks[0])
		}
		if blocks[1].Type != "tool_use" {
			t.Errorf("block[1] = %+v, want tool_use", blocks[1])
		}
	})
}

func TestAnthropicConvertToolResults(t *testing.T) {
	a := NewAnthropic("test-key", "")

	req := &ChatCompletionRequest{
		Model: "claude-sonnet-4-6",
		Messages: []Message{
			{Role: "user", Content: "weather?"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call_a", Type: "function", Function: FunctionCall{Name: "get_weather", Arguments: `{"city":"NYC"}`}},
				{ID: "call_b", Type: "function", Function: FunctionCall{Name: "get_time", Arguments: `{}`}},
			}},
			{Role: "tool", ToolCallID: "call_a", Content: "sunny"},
			{Role: "tool", ToolCallID: "call_b", Content: "noon"},
			{Role: "user", Content: "thanks"},
		},
	}

	ar := a.translateRequest(req)

	// expected: user(weather?), assistant(2 tool_use), user(2 tool_result), user(thanks)
	if len(ar.Messages) != 4 {
		t.Fatalf("messages = %d, want 4: %+v", len(ar.Messages), ar.Messages)
	}

	if ar.Messages[2].Role != "user" {
		t.Errorf("messages[2].Role = %q, want user", ar.Messages[2].Role)
	}
	results, ok := ar.Messages[2].Content.([]anthropicContentBlock)
	if !ok {
		t.Fatalf("messages[2].Content type = %T", ar.Messages[2].Content)
	}
	if len(results) != 2 {
		t.Fatalf("tool_result blocks = %d, want 2 (coalesced)", len(results))
	}
	if results[0].Type != "tool_result" || results[0].ToolUseID != "call_a" || results[0].Content != "sunny" {
		t.Errorf("results[0] = %+v", results[0])
	}
	if results[1].ToolUseID != "call_b" || results[1].Content != "noon" {
		t.Errorf("results[1] = %+v", results[1])
	}

	if s, _ := ar.Messages[3].Content.(string); s != "thanks" {
		t.Errorf("messages[3].Content = %v, want 'thanks'", ar.Messages[3].Content)
	}
}

func TestAnthropicTranslateResponseToolUse(t *testing.T) {
	a := NewAnthropic("test-key", "")

	t.Run("tool use only", func(t *testing.T) {
		resp := &anthropicResponse{
			ID:   "msg_1",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{Type: "tool_use", ID: "toolu_1", Name: "get_weather", Input: json.RawMessage(`{"city":"NYC"}`)},
			},
			StopReason: "tool_use",
		}
		result := a.translateResponse(resp, "claude-sonnet-4-6")
		choice := result.Choices[0]
		if choice.FinishReason == nil || *choice.FinishReason != "tool_calls" {
			t.Errorf("FinishReason = %v, want tool_calls", choice.FinishReason)
		}
		if choice.Message.Content != nil {
			t.Errorf("Content = %v, want nil", choice.Message.Content)
		}
		if len(choice.Message.ToolCalls) != 1 {
			t.Fatalf("ToolCalls = %d, want 1", len(choice.Message.ToolCalls))
		}
		tc := choice.Message.ToolCalls[0]
		if tc.ID != "toolu_1" || tc.Type != "function" || tc.Function.Name != "get_weather" {
			t.Errorf("toolcall = %+v", tc)
		}
		if tc.Function.Arguments != `{"city":"NYC"}` {
			t.Errorf("arguments = %q", tc.Function.Arguments)
		}
	})

	t.Run("mixed text and tool use", func(t *testing.T) {
		resp := &anthropicResponse{
			ID:   "msg_2",
			Role: "assistant",
			Content: []anthropicContentBlock{
				{Type: "text", Text: "checking now"},
				{Type: "tool_use", ID: "toolu_2", Name: "get_weather", Input: json.RawMessage(`{}`)},
			},
			StopReason: "tool_use",
		}
		result := a.translateResponse(resp, "claude-sonnet-4-6")
		msg := result.Choices[0].Message
		if s, _ := msg.Content.(string); s != "checking now" {
			t.Errorf("Content = %v, want 'checking now'", msg.Content)
		}
		if len(msg.ToolCalls) != 1 {
			t.Errorf("ToolCalls = %d, want 1", len(msg.ToolCalls))
		}
	})
}

func TestAnthropicStreamToolCalls(t *testing.T) {
	a := NewAnthropic("test-key", "")

	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"NYC\"}"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go a.readSSEStream(io.NopCloser(strings.NewReader(sse)), events, "claude-sonnet-4-6", false)

	var chunks []*StreamChunk
	var done bool
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
		if ev.Done {
			done = true
			continue
		}
		if ev.Chunk != nil {
			chunks = append(chunks, ev.Chunk)
		}
	}

	if !done {
		t.Error("stream did not emit Done")
	}
	if len(chunks) != 4 {
		t.Fatalf("chunks = %d, want 4: %+v", len(chunks), chunks)
	}

	// opening tool chunk: index + id + type + name, empty arguments
	open := chunks[0].Choices[0].Delta.ToolCalls
	if len(open) != 1 {
		t.Fatalf("opening tool_calls = %d, want 1", len(open))
	}
	if open[0].Index == nil || *open[0].Index != 0 {
		t.Errorf("opening index = %v, want 0", open[0].Index)
	}
	if open[0].ID != "toolu_1" || open[0].Type != "function" || open[0].Function.Name != "get_weather" {
		t.Errorf("opening tool_call = %+v", open[0])
	}
	if open[0].Function.Arguments != "" {
		t.Errorf("opening arguments = %q, want empty", open[0].Function.Arguments)
	}

	// argument fragments share the index, leak no other fields, concat to valid JSON
	var args string
	for _, c := range chunks[1:3] {
		tc := c.Choices[0].Delta.ToolCalls
		if len(tc) != 1 {
			t.Fatalf("fragment tool_calls = %d, want 1", len(tc))
		}
		if tc[0].Index == nil || *tc[0].Index != 0 {
			t.Errorf("fragment index = %v, want 0", tc[0].Index)
		}
		if tc[0].ID != "" || tc[0].Type != "" || tc[0].Function.Name != "" {
			t.Errorf("fragment leaked fields: %+v", tc[0])
		}
		args += tc[0].Function.Arguments
	}
	if args != `{"city":"NYC"}` {
		t.Errorf("concatenated args = %q, want %q", args, `{"city":"NYC"}`)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		t.Errorf("args not valid json: %v", err)
	}

	// final chunk carries finish_reason
	if fr := chunks[3].Choices[0].FinishReason; fr == nil || *fr != "tool_calls" {
		t.Errorf("final FinishReason = %v, want tool_calls", fr)
	}
}

// TestAnthropicStreamUsage pins the billing fix: with stream_options.include_usage the
// stream ends with a usage-bearing chunk built from anthropic's input_tokens
// (message_start) and output_tokens (message_delta). Without it, no usage chunk is emitted.
func TestAnthropicStreamUsage(t *testing.T) {
	a := NewAnthropic("test-key", "")
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":57}}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	collect := func(includeUsage bool) []*StreamChunk {
		events := make(chan StreamEvent, 10)
		go a.readSSEStream(io.NopCloser(strings.NewReader(sse)), events, "claude-sonnet-5", includeUsage)
		var chunks []*StreamChunk
		for ev := range events {
			if ev.Err != nil {
				t.Fatalf("stream error: %v", ev.Err)
			}
			if ev.Chunk != nil {
				chunks = append(chunks, ev.Chunk)
			}
		}
		return chunks
	}

	// include_usage=true: the last chunk carries usage with empty choices.
	withUsage := collect(true)
	last := withUsage[len(withUsage)-1]
	if last.Usage == nil {
		t.Fatal("include_usage=true: final chunk has no usage")
	}
	if last.Usage.PromptTokens != 57 || last.Usage.CompletionTokens != 42 || last.Usage.TotalTokens != 99 {
		t.Fatalf("usage = %+v, want prompt=57 completion=42 total=99", last.Usage)
	}
	if len(last.Choices) != 0 {
		t.Errorf("usage chunk should have empty choices, got %d", len(last.Choices))
	}

	// include_usage=false: no chunk carries usage.
	for _, c := range collect(false) {
		if c.Usage != nil {
			t.Fatalf("include_usage=false must not emit usage, got %+v", c.Usage)
		}
	}
}

func TestAnthropicStreamErrorEvent(t *testing.T) {
	a := NewAnthropic("test-key", "")

	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_1"}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`,
		``,
		`data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go a.readSSEStream(io.NopCloser(strings.NewReader(sse)), events, "claude-sonnet-4-6", false)

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
		t.Fatal("mid-stream error event must surface as a stream error")
	}
	if !strings.Contains(streamErr.Error(), "Overloaded") {
		t.Errorf("stream error = %q, want the upstream message", streamErr)
	}
	if done {
		t.Error("an errored stream must not also emit Done")
	}
}

func TestAnthropicStreamToolCallIndexMapping(t *testing.T) {
	a := NewAnthropic("test-key", "")

	// text block at anthropic index 0, tool_use at anthropic index 1; the OpenAI
	// tool_call index must still be 0 (it counts only tool calls).
	sse := strings.Join([]string{
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_9","name":"f"}}`,
		``,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	events := make(chan StreamEvent, 10)
	go a.readSSEStream(io.NopCloser(strings.NewReader(sse)), events, "m", false)

	var textContent string
	var toolIdx *int
	for ev := range events {
		if ev.Chunk == nil {
			continue
		}
		d := ev.Chunk.Choices[0].Delta
		if d.Content != "" {
			textContent += d.Content
		}
		if len(d.ToolCalls) > 0 && d.ToolCalls[0].Index != nil {
			toolIdx = d.ToolCalls[0].Index
		}
	}

	if textContent != "hi" {
		t.Errorf("text = %q, want hi", textContent)
	}
	if toolIdx == nil || *toolIdx != 0 {
		t.Errorf("tool index = %v, want 0 (first tool despite anthropic block index 1)", toolIdx)
	}
}

func TestAnthropicStreamFragmentWireShape(t *testing.T) {
	idx := 0
	// a streaming argument fragment should serialize as
	// {"index":N,"function":{"arguments":"..."}} with no empty id/type/name.
	tc := ToolCall{
		Index:    &idx,
		Function: FunctionCall{Arguments: `{"a":1}`},
	}
	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"id"`) || strings.Contains(s, `"type"`) || strings.Contains(s, `"name"`) {
		t.Errorf("fragment tool_call leaked empty fields: %s", s)
	}
}
