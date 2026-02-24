package routing

import (
	"regexp"
	"strings"
)

// compiledRule pairs a HeuristicRule with precompiled keyword and exclusion patterns.
type compiledRule struct {
	rule       HeuristicRule
	patterns   []*regexp.Regexp
	exclusions []*regexp.Regexp
}

// HeuristicMatcher evaluates heuristic rules against a request.
type HeuristicMatcher struct {
	compiled []compiledRule
}

// NewHeuristicMatcher creates a new HeuristicMatcher with the given rules.
// keyword patterns are compiled with word boundary anchors for accurate matching.
func NewHeuristicMatcher(rules []HeuristicRule) *HeuristicMatcher {
	compiled := make([]compiledRule, len(rules))
	for i, rule := range rules {
		patterns := make([]*regexp.Regexp, len(rule.Match.Keywords))
		for j, kw := range rule.Match.Keywords {
			patterns[j] = regexp.MustCompile(keywordPattern(kw))
		}
		exclusions := make([]*regexp.Regexp, len(rule.Match.Exclude))
		for j, ex := range rule.Match.Exclude {
			exclusions[j] = regexp.MustCompile(keywordPattern(ex))
		}
		compiled[i] = compiledRule{rule: rule, patterns: patterns, exclusions: exclusions}
	}
	return &HeuristicMatcher{compiled: compiled}
}

// Match returns the route name of the first matching rule, or "" if no rules match.
func (h *HeuristicMatcher) Match(info *RequestInfo) string {
	for _, cr := range h.compiled {
		if h.matchRule(&cr, info) {
			return cr.rule.Route
		}
	}
	return ""
}

func (h *HeuristicMatcher) matchRule(cr *compiledRule, info *RequestInfo) bool {
	cond := &cr.rule.Match

	if len(cr.patterns) > 0 {
		if !h.matchKeywords(cr.patterns, cr.exclusions, info) {
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

// keywordPattern builds a case-insensitive regex for a keyword with word boundary
// anchors. boundaries are only added at edges that touch a word character, so keywords
// like "c++" work correctly.
func keywordPattern(kw string) string {
	quoted := regexp.QuoteMeta(kw)
	prefix := ""
	suffix := ""
	if len(kw) > 0 && isWordChar(kw[0]) {
		prefix = `\b`
	}
	if len(kw) > 0 && isWordChar(kw[len(kw)-1]) {
		suffix = `\b`
	}
	return `(?i)` + prefix + quoted + suffix
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func (h *HeuristicMatcher) matchKeywords(patterns, exclusions []*regexp.Regexp, info *RequestInfo) bool {
	// build combined text from user messages only; system prompts are matched
	// separately via the system_prompt_contains condition
	var combined strings.Builder
	for _, msg := range info.Messages {
		if strings.ToLower(msg.Role) == "user" {
			combined.WriteString(msg.Content)
			combined.WriteByte(' ')
		}
	}
	text := combined.String()

	// check exclusions first; if any exclusion phrase is present, skip keywords
	for _, ex := range exclusions {
		if ex.MatchString(text) {
			return false
		}
	}

	for _, p := range patterns {
		if p.MatchString(text) {
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
