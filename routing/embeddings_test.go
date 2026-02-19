package routing

import (
	"context"
	"math"
	"testing"
)

// mockEmbedder returns deterministic vectors for testing.
type mockEmbedder struct {
	vectors map[string][]float64
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if v, ok := m.vectors[text]; ok {
		return v, nil
	}
	// return a default vector
	return []float64{0, 0, 0}, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := m.Embed(ctx, t)
		if err != nil {
			return nil, err
		}
		result[i] = v
	}
	return result, nil
}

func TestEmbeddingMatcherCentroid(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float64{
			// coding route exemplars
			"write a function": {1, 0, 0},
			"debug this code":  {0.9, 0.1, 0},
			// creative route exemplars
			"write a poem":    {0, 1, 0},
			"tell me a story": {0, 0.9, 0.1},
			// query: closer to coding
			"fix this bug": {0.95, 0.05, 0},
		},
	}

	routes := []RouteConfig{
		{Name: "coding", Model: "gpt-4", Examples: []string{"write a function", "debug this code"}},
		{Name: "creative", Model: "claude-3", Examples: []string{"write a poem", "tell me a story"}},
	}

	cfg := &SemanticConfig{
		Enabled:    true,
		Threshold:  0.8,
		Comparison: "centroid",
	}

	em, err := NewEmbeddingMatcher(context.Background(), embedder, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "fix this bug"}},
	}

	route, confidence, err := em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "coding" {
		t.Errorf("expected route 'coding', got '%s'", route)
	}
	if confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", confidence)
	}
}

func TestEmbeddingMatcherMax(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float64{
			"example1":    {1, 0, 0},
			"example2":    {0.5, 0.5, 0},
			"test prompt": {0.99, 0.01, 0},
		},
	}

	routes := []RouteConfig{
		{Name: "route1", Model: "model1", Examples: []string{"example1", "example2"}},
	}

	cfg := &SemanticConfig{
		Enabled:    true,
		Threshold:  0.8,
		Comparison: "max",
	}

	em, err := NewEmbeddingMatcher(context.Background(), embedder, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test prompt"}},
	}

	route, confidence, err := em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "route1" {
		t.Errorf("expected route 'route1', got '%s'", route)
	}
	if confidence < 0.99 {
		t.Errorf("expected very high confidence with max comparison, got %f", confidence)
	}
}

func TestEmbeddingMatcherAverage(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float64{
			"a":     {1, 0},
			"b":     {0.8, 0.2},
			"query": {0.9, 0.1},
		},
	}

	routes := []RouteConfig{
		{Name: "r1", Model: "m1", Examples: []string{"a", "b"}},
	}

	cfg := &SemanticConfig{
		Enabled:    true,
		Threshold:  0.5,
		Comparison: "average",
	}

	em, err := NewEmbeddingMatcher(context.Background(), embedder, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "query"}},
	}

	route, confidence, err := em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "r1" {
		t.Errorf("expected 'r1', got '%s'", route)
	}
	if math.IsNaN(confidence) || confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", confidence)
	}
}

func TestEmbeddingMatcherNoUserMessage(t *testing.T) {
	embedder := &mockEmbedder{vectors: map[string][]float64{}}

	routes := []RouteConfig{
		{Name: "r1", Model: "m1", Examples: []string{"example"}},
	}

	cfg := &SemanticConfig{Enabled: true, Comparison: "centroid"}

	em, err := NewEmbeddingMatcher(context.Background(), embedder, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "system", Content: "you are helpful"}},
	}

	route, _, err := em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if route != "" {
		t.Errorf("expected empty route for no user message, got '%s'", route)
	}
}

// countingEmbedder wraps an Embedder and counts Embed calls.
type countingEmbedder struct {
	inner      Embedder
	embedCount int
}

func (c *countingEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	c.embedCount++
	return c.inner.Embed(ctx, text)
}

func (c *countingEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	return c.inner.EmbedBatch(ctx, texts)
}

func TestEmbeddingMatcherCache(t *testing.T) {
	inner := &mockEmbedder{
		vectors: map[string][]float64{
			"write code":    {1, 0},
			"fix bugs":      {0.9, 0.1},
			"debug my code": {0.95, 0.05},
		},
	}
	counter := &countingEmbedder{inner: inner}

	routes := []RouteConfig{
		{Name: "coding", Model: "gpt-4", Examples: []string{"write code", "fix bugs"}},
	}

	cfg := &SemanticConfig{
		Enabled:         true,
		Threshold:       0.8,
		Comparison:      "centroid",
		CacheEmbeddings: true,
		CacheTTL:        3600,
		CacheSize:       100,
	}

	em, err := NewEmbeddingMatcher(context.Background(), counter, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "debug my code"}},
	}

	// first call: should call Embed
	counter.embedCount = 0
	_, _, err = em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("first match error: %v", err)
	}
	if counter.embedCount != 1 {
		t.Errorf("expected 1 embed call, got %d", counter.embedCount)
	}

	// second call with same input: should hit cache
	counter.embedCount = 0
	_, _, err = em.Match(context.Background(), info)
	if err != nil {
		t.Fatalf("second match error: %v", err)
	}
	if counter.embedCount != 0 {
		t.Errorf("expected 0 embed calls (cache hit), got %d", counter.embedCount)
	}
}

func TestEmbeddingMatcherNoExamples(t *testing.T) {
	embedder := &mockEmbedder{vectors: map[string][]float64{}}

	routes := []RouteConfig{
		{Name: "r1", Model: "m1"}, // no examples
	}

	cfg := &SemanticConfig{Enabled: true, Comparison: "centroid"}

	em, err := NewEmbeddingMatcher(context.Background(), embedder, routes, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(em.routes) != 0 {
		t.Errorf("expected no embedded routes, got %d", len(em.routes))
	}
}
