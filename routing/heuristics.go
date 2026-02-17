package routing

import "strings"

// HeuristicMatcher evaluates heuristic rules against a request.
type HeuristicMatcher struct {
	rules []HeuristicRule
}

// NewHeuristicMatcher creates a new HeuristicMatcher with the given rules.
func NewHeuristicMatcher(rules []HeuristicRule) *HeuristicMatcher {
	return &HeuristicMatcher{rules: rules}
}

// Match returns the route name of the first matching rule, or "" if no rules match.
func (h *HeuristicMatcher) Match(info *RequestInfo) string {
	for _, rule := range h.rules {
		if h.matchRule(&rule.Match, info) {
			return rule.Route
		}
	}
	return ""
}

func (h *HeuristicMatcher) matchRule(cond *MatchCondition, info *RequestInfo) bool {
	if len(cond.Keywords) > 0 {
		if !h.matchKeywords(cond.Keywords, info) {
			return false
		}
	}

	if cond.SystemPromptContains != "" {
		if !h.matchSystemPrompt(cond.SystemPromptContains, info) {
			return false
		}
	}

	if cond.MaxTokensLt != nil {
		if info.MaxTokens == nil || *info.MaxTokens >= *cond.MaxTokensLt {
			return false
		}
	}

	if cond.MessageLengthLt != nil {
		totalLen := 0
		for _, msg := range info.Messages {
			totalLen += len(msg.Content)
		}
		if totalLen >= *cond.MessageLengthLt {
			return false
		}
	}

	if cond.HasTools != nil {
		if info.HasTools != *cond.HasTools {
			return false
		}
	}

	return true
}

func (h *HeuristicMatcher) matchKeywords(keywords []string, info *RequestInfo) bool {
	// build a combined text from all messages
	var combined strings.Builder
	for _, msg := range info.Messages {
		combined.WriteString(strings.ToLower(msg.Content))
		combined.WriteByte(' ')
	}
	text := combined.String()

	for _, kw := range keywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func (h *HeuristicMatcher) matchSystemPrompt(substr string, info *RequestInfo) bool {
	for _, msg := range info.Messages {
		if msg.Role == "system" {
			if strings.Contains(strings.ToLower(msg.Content), strings.ToLower(substr)) {
				return true
			}
		}
	}
	return false
}
