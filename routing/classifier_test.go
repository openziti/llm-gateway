package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClassifierMatcherSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := classifierChatResponse{
			Choices: []classifierChoice{
				{
					Message: classifierMessage{
						Role:    "assistant",
						Content: `{"category": "coding", "confidence": 0.95}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &ClassifierConfig{
		Enabled:             true,
		Model:               "llama3",
		ConfidenceThreshold: 0.5,
	}

	routes := []RouteConfig{
		{Name: "coding", Model: "gpt-4", Description: "code generation and debugging"},
		{Name: "creative", Model: "claude-3", Description: "creative writing"},
	}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "")

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "fix this bug in my code"}},
	}

	route, confidence, err := cm.Classify(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "coding" {
		t.Errorf("expected 'coding', got '%s'", route)
	}
	if confidence != 0.95 {
		t.Errorf("expected 0.95, got %f", confidence)
	}
}

func TestClassifierMatcherMarkdownWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := classifierChatResponse{
			Choices: []classifierChoice{
				{
					Message: classifierMessage{
						Role: "assistant",
						Content: "```json\n{\"category\": \"creative\", \"confidence\": 0.8}\n```",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &ClassifierConfig{Enabled: true, Model: "llama3"}
	routes := []RouteConfig{
		{Name: "creative", Model: "claude-3", Description: "creative writing"},
	}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "")

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "write a poem"}},
	}

	route, _, err := cm.Classify(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "creative" {
		t.Errorf("expected 'creative', got '%s'", route)
	}
}

func TestClassifierMatcherUnknownCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := classifierChatResponse{
			Choices: []classifierChoice{
				{
					Message: classifierMessage{
						Role:    "assistant",
						Content: `{"category": "unknown_route", "confidence": 0.9}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &ClassifierConfig{Enabled: true, Model: "llama3"}
	routes := []RouteConfig{
		{Name: "coding", Model: "gpt-4", Description: "coding"},
	}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "")
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test"}},
	}

	_, _, err := cm.Classify(context.Background(), info)
	if err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func TestClassifierMatcherServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	cfg := &ClassifierConfig{Enabled: true, Model: "llama3"}
	routes := []RouteConfig{{Name: "coding", Model: "gpt-4"}}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "")
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test"}},
	}

	_, _, err := cm.Classify(context.Background(), info)
	if err == nil {
		t.Fatal("expected error on server error")
	}
}

func TestClassifierMatcherCaseInsensitiveCategory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := classifierChatResponse{
			Choices: []classifierChoice{
				{
					Message: classifierMessage{
						Role:    "assistant",
						Content: `{"category": "Coding", "confidence": 0.9}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &ClassifierConfig{Enabled: true, Model: "llama3"}
	routes := []RouteConfig{{Name: "coding", Model: "gpt-4", Description: "coding"}}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "")
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test"}},
	}

	route, _, err := cm.Classify(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "coding" {
		t.Errorf("expected 'coding', got '%s'", route)
	}
}

func TestClassifierMatcherWithAuth(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		resp := classifierChatResponse{
			Choices: []classifierChoice{
				{
					Message: classifierMessage{
						Role:    "assistant",
						Content: `{"category": "coding", "confidence": 0.9}`,
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &ClassifierConfig{Enabled: true, Model: "gpt-4"}
	routes := []RouteConfig{{Name: "coding", Model: "gpt-4", Description: "coding"}}

	cm := NewClassifierMatcher(cfg, routes, server.URL, "sk-test")
	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test"}},
	}

	cm.Classify(context.Background(), info)
	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected 'Bearer sk-test', got '%s'", gotAuth)
	}
}
