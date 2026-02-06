package providers

import (
	"encoding/json"
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
	if choice.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", choice.FinishReason, "stop")
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
