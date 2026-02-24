package gateway

import (
	"context"
	"net/http"
	"path"
	"strings"

	"github.com/openziti/llm-gateway/providers"
)

// future option: store hashed keys (e.g. SHA-256) instead of plaintext.
// this would require hashing incoming bearer tokens before lookup and would
// prevent key recovery from the config file, but avoids storing secrets
// in plaintext on disk.

type contextKey int

const apiKeyContextKey contextKey = iota

// KeyStore holds the set of valid API keys and provides middleware for authentication.
type KeyStore struct {
	keys map[string]*APIKeyEntry
}

// NewKeyStore builds a key lookup map from config entries.
func NewKeyStore(entries []APIKeyEntry) *KeyStore {
	ks := &KeyStore{
		keys: make(map[string]*APIKeyEntry, len(entries)),
	}
	for i := range entries {
		ks.keys[entries[i].Key] = &entries[i]
	}
	return ks
}

// Middleware returns an http.Handler that enforces bearer token authentication.
// Requests to /health and /metrics pass through without auth.
func (ks *KeyStore) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			providers.WriteError(w,
				providers.NewAPIError("API key required", providers.ErrorTypeAuthentication),
				http.StatusUnauthorized,
			)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		entry, ok := ks.keys[token]
		if !ok {
			providers.WriteError(w, providers.ErrUnauthorized, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), apiKeyContextKey, entry)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// KeyFromContext retrieves the API key entry from the request context.
// Returns nil when authentication is disabled or the request was not authenticated.
func KeyFromContext(ctx context.Context) *APIKeyEntry {
	entry, _ := ctx.Value(apiKeyContextKey).(*APIKeyEntry)
	return entry
}

// CheckModel reports whether the key is allowed to use the given model.
// A key with no AllowedModels restrictions permits all models.
func CheckModel(entry *APIKeyEntry, model string) bool {
	if len(entry.AllowedModels) == 0 {
		return true
	}
	for _, pattern := range entry.AllowedModels {
		if matched, _ := path.Match(pattern, model); matched {
			return true
		}
	}
	return false
}

// CheckRoute reports whether the key is allowed to use the given semantic route.
// A key with no AllowedRoutes restrictions permits all routes.
func CheckRoute(entry *APIKeyEntry, route string) bool {
	if len(entry.AllowedRoutes) == 0 {
		return true
	}
	for _, allowed := range entry.AllowedRoutes {
		if allowed == route {
			return true
		}
	}
	return false
}
