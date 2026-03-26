package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedClientLocal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req localEmbedRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Model != "nomic-embed-text" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		resp := localEmbedResponse{
			Embeddings: [][]float64{
				{0.1, 0.2, 0.3},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbedClient("local", "nomic-embed-text", server.URL, "")
	vec, err := client.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector: %v", vec)
	}
}

func TestEmbedClientLocalBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := localEmbedResponse{
			Embeddings: [][]float64{
				{0.1, 0.2},
				{0.3, 0.4},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbedClient("local", "nomic-embed-text", server.URL, "")
	vecs, err := client.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestEmbedClientOpenAI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		resp := openaiEmbedResponse{
			Data: []openaiEmbedding{
				{Embedding: []float64{0.5, 0.6, 0.7}, Index: 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbedClient("openai", "text-embedding-3-small", server.URL, "test-key")
	vec, err := client.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.5 {
		t.Errorf("unexpected first value: %f", vec[0])
	}
}

func TestEmbedClientOpenAIBatchOrdering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// return in reverse order to test index-based reordering
		resp := openaiEmbedResponse{
			Data: []openaiEmbedding{
				{Embedding: []float64{0.3, 0.4}, Index: 1},
				{Embedding: []float64{0.1, 0.2}, Index: 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewEmbedClient("openai", "text-embedding-3-small", server.URL, "test-key")
	vecs, err := client.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vecs[0][0] != 0.1 || vecs[1][0] != 0.3 {
		t.Errorf("ordering incorrect: %v", vecs)
	}
}

func TestEmbedClientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewEmbedClient("local", "nomic-embed-text", server.URL, "")
	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbedClientUnsupportedProvider(t *testing.T) {
	client := NewEmbedClient("unknown", "model", "http://localhost", "")
	_, err := client.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
}

func TestEmbedClientWithHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := localEmbedResponse{
			Embeddings: [][]float64{{1.0, 2.0}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	customClient := &http.Client{}
	client := NewEmbedClientWithHTTPClient("local", "test", server.URL, "", customClient)
	vec, err := client.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(vec))
	}
}
