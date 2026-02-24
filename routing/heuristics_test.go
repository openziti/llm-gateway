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

func TestHeuristicWordBoundary(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"code"}},
			Route: "coding",
		},
	})

	// "code" should NOT match inside "unicode"
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "explain unicode encoding"}},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("word boundary false positive = %q, want empty", got)
	}

	// "code" should match as a standalone word
	info = &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "write some code for me"}},
	}
	if got := m.Match(info); got != "coding" {
		t.Errorf("word boundary match = %q, want 'coding'", got)
	}
}

func TestHeuristicMultiWordKeyword(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"step by step"}},
			Route: "detailed",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "explain step by step how to bake a cake"}},
	}
	if got := m.Match(info); got != "detailed" {
		t.Errorf("multi-word keyword = %q, want 'detailed'", got)
	}

	info = &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "take the next step"}},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("multi-word keyword partial = %q, want empty", got)
	}
}

func TestHeuristicKeywordsIgnoreSystemPrompt(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"translate", "translation"}},
			Route: "general",
		},
	})

	// system prompt contains "translation" but user message does not
	info := &RequestInfo{
		Messages: []MessageInfo{
			{Role: "system", Content: "You are a helpful assistant for coding, translation, and more"},
			{Role: "user", Content: "how does gravity work on the moon"},
		},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("keyword in system prompt should not match, got %q", got)
	}

	// same system prompt, but user message does contain the keyword
	info = &RequestInfo{
		Messages: []MessageInfo{
			{Role: "system", Content: "You are a helpful assistant for coding, translation, and more"},
			{Role: "user", Content: "translate this to French"},
		},
	}
	if got := m.Match(info); got != "general" {
		t.Errorf("keyword in user message should match, got %q", got)
	}
}

func TestHeuristicExclude(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{
				Keywords: []string{"code"},
				Exclude:  []string{"code fences", "code block"},
			},
			Route: "coding",
		},
	})

	// "code" present but "code fences" triggers exclusion
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "without any markdown code fences, generate a title"}},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("exclude should suppress match, got %q", got)
	}

	// "code" present, no exclusion phrase
	info = &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "write some code for me"}},
	}
	if got := m.Match(info); got != "coding" {
		t.Errorf("no exclusion present = %q, want 'coding'", got)
	}

	// "code block" exclusion
	info = &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "do not use a code block in your response"}},
	}
	if got := m.Match(info); got != "" {
		t.Errorf("code block exclusion should suppress match, got %q", got)
	}
}

func TestHeuristicExcludeEmpty(t *testing.T) {
	// no exclusions configured, keywords work normally
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"code"}},
			Route: "coding",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "write code for me"}},
	}
	if got := m.Match(info); got != "coding" {
		t.Errorf("empty exclude list = %q, want 'coding'", got)
	}
}

func TestHeuristicSpecialCharKeyword(t *testing.T) {
	m := NewHeuristicMatcher([]HeuristicRule{
		{
			Match: MatchCondition{Keywords: []string{"c++"}},
			Route: "coding",
		},
	})

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "write a c++ program"}},
	}
	if got := m.Match(info); got != "coding" {
		t.Errorf("special char keyword = %q, want 'coding'", got)
	}
}
