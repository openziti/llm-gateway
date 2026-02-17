package routing

import "testing"

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func TestHeuristicKeywordMatch(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"translate", "translation"}},
			Route: "fast",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "user", Content: "Please translate this text to French"},
		},
	}
	if got := m.Match(info); got != "fast" {
		t.Errorf("keyword match = %q, want 'fast'", got)
	}
}

func TestHeuristicKeywordNoMatch(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"translate"}},
			Route: "fast",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "user", Content: "Write me a poem about the ocean"},
		},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("keyword no match = %q, want empty", got)
	}
}

func TestHeuristicKeywordCaseInsensitive(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"TRANSLATE"}},
			Route: "fast",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "user", Content: "translate this"},
		},
	}
	if got := m.Match(info); got != "fast" {
		t.Errorf("case insensitive match = %q, want 'fast'", got)
	}
}

func TestHeuristicSystemPrompt(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{SystemPromptContains: "you are a code assistant"},
			Route: "coding",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "system", Content: "You are a code assistant for Python"},
			{Role: "user", Content: "Fix this bug"},
		},
	}
	if got := m.Match(info); got != "coding" {
		t.Errorf("system prompt match = %q, want 'coding'", got)
	}
}

func TestHeuristicMaxTokens(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{MaxTokensLt: intPtr(100)},
			Route: "fast",
		},
	})

	// matches: max_tokens < 100
	info := &RequestInfo{MaxTokens: intPtr(50)}
	if got := m.Match(info); got != "fast" {
		t.Errorf("max_tokens match = %q, want 'fast'", got)
	}

	// no match: max_tokens >= 100
	info = &RequestInfo{MaxTokens: intPtr(200)}
	if got := m.Match(info); got != "" {
		t.Errorf("max_tokens no match = %q, want empty", got)
	}

	// no match: max_tokens nil
	info = &RequestInfo{}
	if got := m.Match(info); got != "" {
		t.Errorf("max_tokens nil = %q, want empty", got)
	}
}

func TestHeuristicMessageLength(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{MessageLengthLt: intPtr(20)},
			Route: "fast",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "user", Content: "Hi"},
		},
	}
	if got := m.Match(info); got != "fast" {
		t.Errorf("short message match = %q, want 'fast'", got)
	}

	info = &RequestInfo{
		Messages: []MessageInfo{
			{Role: "user", Content: "This is a much longer message that exceeds the limit"},
		},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("long message = %q, want empty", got)
	}
}

func TestHeuristicHasTools(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{HasTools: boolPtr(true)},
			Route: "tool-capable",
		},
	})

	info := &RequestInfo{HasTools: true}
	if got := m.Match(info); got != "tool-capable" {
		t.Errorf("has tools match = %q, want 'tool-capable'", got)
	}

	info = &RequestInfo{HasTools: false}
	if got := m.Match(info); got != "" {
		t.Errorf("no tools = %q, want empty", got)
	}
}

func TestHeuristicANDLogic(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{
				Keywords: []string{"code"},
				HasTools: boolPtr(true),
			},
			Route: "coding-with-tools",
		},
	})

	// both conditions met
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "Write code for me"}},
		HasTools: true,
	}
	if got := m.Match(info); got != "coding-with-tools" {
		t.Errorf("AND both met = %q, want 'coding-with-tools'", got)
	}

	// only keyword met
	info = &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "Write code for me"}},
		HasTools: false,
	}
	if got := m.Match(info); got != "" {
		t.Errorf("AND partial = %q, want empty", got)
	}
}

func TestHeuristicFirstMatchWins(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"code"}},
			Route: "first",
		},
		{
			Match: MatchCondition{Keywords: []string{"code"}},
			Route: "second",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "Write code"}},
	}
	if got := m.Match(info); got != "first" {
		t.Errorf("first match wins = %q, want 'first'", got)
	}
}

func TestHeuristicNoRules(t *testing.T) {
	m := NewHeuristicMatcher(nil)
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("no rules = %q, want empty", got)
	}
}
