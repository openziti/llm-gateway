package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openziti/llm-gateway/providers"
)

func TestKeyStoreLookup(t *testing.T) {
	ks := NewKeyStore([]APIKeyEntry{
		{Name: "alice", Key: "sk-gw-aaa"},
		{Name: "bob", Key: "sk-gw-bbb"},
	})

	if entry, ok := ks.keys["sk-gw-aaa"]; !ok || entry.Name != "alice" {
		t.Fatal("expected to find alice")
	}
	if entry, ok := ks.keys["sk-gw-bbb"]; !ok || entry.Name != "bob" {
		t.Fatal("expected to find bob")
	}
	if _, ok := ks.keys["sk-gw-ccc"]; ok {
		t.Fatal("expected unknown key to be absent")
	}
}

func TestCheckModel(t *testing.T) {
	tests := []struct {
		name    string
		entry   APIKeyEntry
		model   string
		allowed bool
	}{
		{"no restrictions", APIKeyEntry{AllowedModels: nil}, "gpt-4", true},
		{"empty restrictions", APIKeyEntry{AllowedModels: []string{}}, "gpt-4", true},
		{"wildcard", APIKeyEntry{AllowedModels: []string{"*"}}, "anything", true},
		{"glob match", APIKeyEntry{AllowedModels: []string{"claude-*"}}, "claude-haiku-4-5-20251001", true},
		{"glob no match", APIKeyEntry{AllowedModels: []string{"claude-*"}}, "gpt-4", false},
		{"exact match", APIKeyEntry{AllowedModels: []string{"llama3"}}, "llama3", true},
		{"exact no match", APIKeyEntry{AllowedModels: []string{"llama3"}}, "llama2", false},
		{"multiple patterns", APIKeyEntry{AllowedModels: []string{"claude-*", "gpt-*"}}, "gpt-4", true},
		{"multiple patterns no match", APIKeyEntry{AllowedModels: []string{"claude-*", "gpt-*"}}, "llama3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckModel(&tt.entry, tt.model); got != tt.allowed {
				t.Errorf("CheckModel(%v, %q) = %v, want %v", tt.entry.AllowedModels, tt.model, got, tt.allowed)
			}
		})
	}
}

func TestCheckRoute(t *testing.T) {
	tests := []struct {
		name    string
		entry   APIKeyEntry
		route   string
		allowed bool
	}{
		{"no restrictions", APIKeyEntry{AllowedRoutes: nil}, "coding", true},
		{"empty restrictions", APIKeyEntry{AllowedRoutes: []string{}}, "coding", true},
		{"allowed", APIKeyEntry{AllowedRoutes: []string{"coding", "general"}}, "coding", true},
		{"not allowed", APIKeyEntry{AllowedRoutes: []string{"coding", "general"}}, "creative", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckRoute(&tt.entry, tt.route); got != tt.allowed {
				t.Errorf("CheckRoute(%v, %q) = %v, want %v", tt.entry.AllowedRoutes, tt.route, got, tt.allowed)
			}
		})
	}
}

func TestMiddlewarePassthroughHealth(t *testing.T) {
	ks := NewKeyStore([]APIKeyEntry{{Name: "test", Key: "sk-gw-test"}})

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := ks.Middleware(inner)

	// /health should pass without auth
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/health returned %d, want 200", rr.Code)
	}

	// /metrics should pass without auth
	req = httptest.NewRequest("GET", "/metrics", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/metrics returned %d, want 200", rr.Code)
	}
}

func TestMiddlewareMissingHeader(t *testing.T) {
	ks := NewKeyStore([]APIKeyEntry{{Name: "test", Key: "sk-gw-test"}})
	handler := ks.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("missing header returned %d, want 401", rr.Code)
	}

	var errResp providers.ErrorResponse
	json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp.Error.Type != providers.ErrorTypeAuthentication {
		t.Errorf("error type = %q, want %q", errResp.Error.Type, providers.ErrorTypeAuthentication)
	}
}

func TestMiddlewareInvalidKey(t *testing.T) {
	ks := NewKeyStore([]APIKeyEntry{{Name: "test", Key: "sk-gw-test"}})
	handler := ks.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-gw-wrong")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("invalid key returned %d, want 401", rr.Code)
	}
}

func TestMiddlewareValidKey(t *testing.T) {
	ks := NewKeyStore([]APIKeyEntry{{Name: "alice", Key: "sk-gw-alice"}})

	var gotEntry *APIKeyEntry
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEntry = KeyFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := ks.Middleware(inner)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-gw-alice")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("valid key returned %d, want 200", rr.Code)
	}
	if gotEntry == nil || gotEntry.Name != "alice" {
		t.Error("expected alice key entry in context")
	}
}

func TestKeyFromContextNil(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if entry := KeyFromContext(req.Context()); entry != nil {
		t.Error("expected nil entry from unauthenticated context")
	}
}
