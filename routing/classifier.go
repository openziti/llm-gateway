package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClassifierMatcher performs LLM-based request classification.
type ClassifierMatcher struct {
	cfg     *ClassifierConfig
	client  *http.Client
	baseURL string
	apiKey  string
	routes  []RouteConfig
}

// NewClassifierMatcher creates a new ClassifierMatcher.
func NewClassifierMatcher(cfg *ClassifierConfig, routes []RouteConfig, baseURL, apiKey string) *ClassifierMatcher {
	return &ClassifierMatcher{
		cfg:     cfg,
		client:  http.DefaultClient,
		baseURL: baseURL,
		apiKey:  apiKey,
		routes:  routes,
	}
}

// NewClassifierMatcherWithHTTPClient creates a new ClassifierMatcher with a custom HTTP client.
func NewClassifierMatcherWithHTTPClient(cfg *ClassifierConfig, routes []RouteConfig, baseURL, apiKey string, client *http.Client) *ClassifierMatcher {
	return &ClassifierMatcher{
		cfg:     cfg,
		client:  client,
		baseURL: baseURL,
		apiKey:  apiKey,
		routes:  routes,
	}
}

// classifierResponse is the expected JSON response from the classifier LLM.
type classifierResponse struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

// Classify sends the request to an LLM for classification and returns the route and confidence.
func (cm *ClassifierMatcher) Classify(ctx context.Context, info *RequestInfo) (string, float64, error) {
	if cm.cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cm.cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	prompt := cm.buildPrompt(info)

	reqBody := classifierChatRequest{
		Model: cm.cfg.Model,
		Messages: []classifierMessage{
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", 0, fmt.Errorf("failed to marshal classifier request: %w", err)
	}

	url := cm.baseURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cm.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+cm.apiKey)
	}

	resp, err := cm.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("classifier request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read classifier response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("classifier error %d: %s", resp.StatusCode, string(respBody))
	}

	// parse the chat completion response
	var chatResp classifierChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", 0, fmt.Errorf("failed to unmarshal classifier response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", 0, fmt.Errorf("classifier returned no choices")
	}

	content := chatResp.Choices[0].Message.Content

	// extract JSON from the response (may be wrapped in markdown code blocks)
	content = extractJSON(content)

	var result classifierResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse classifier output '%s': %w", content, err)
	}

	// validate the category is a known route
	for _, r := range cm.routes {
		if strings.EqualFold(r.Name, result.Category) {
			return r.Name, result.Confidence, nil
		}
	}

	return "", 0, fmt.Errorf("classifier returned unknown category '%s'", result.Category)
}

func (cm *ClassifierMatcher) buildPrompt(info *RequestInfo) string {
	var b strings.Builder
	b.WriteString("Classify the following user request into one of these categories.\n\n")
	b.WriteString("Categories:\n")
	for _, r := range cm.routes {
		b.WriteString(fmt.Sprintf("- %s: %s\n", r.Name, r.Description))
	}
	b.WriteString("\nUser request:\n")
	prompt := lastUserMessage(info)
	if prompt != "" {
		b.WriteString(prompt)
	}
	b.WriteString("\n\nRespond with JSON only: {\"category\": \"<name>\", \"confidence\": <0.0-1.0>}")
	return b.String()
}

// extractJSON strips markdown code block wrappers from JSON content.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

// classifier-specific request/response types (minimal, only what we need)

type classifierChatRequest struct {
	Model    string              `json:"model"`
	Messages []classifierMessage `json:"messages"`
}

type classifierMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type classifierChatResponse struct {
	Choices []classifierChoice `json:"choices"`
}

type classifierChoice struct {
	Message classifierMessage `json:"message"`
}
