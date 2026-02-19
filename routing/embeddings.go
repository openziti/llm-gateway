package routing

import (
	"context"
	"fmt"
	"strings"
)

// routeEmbeddings holds precomputed embeddings for a route's exemplars.
type routeEmbeddings struct {
	name      string
	centroid  []float64
	exemplars [][]float64
}

// EmbeddingMatcher performs embedding-based similarity matching.
type EmbeddingMatcher struct {
	routes     []routeEmbeddings
	client     Embedder
	threshold  float64
	ambiguous  float64
	comparison string
}

// NewEmbeddingMatcher creates a new EmbeddingMatcher, embedding all route exemplars at init.
func NewEmbeddingMatcher(ctx context.Context, client Embedder, routes []RouteConfig, cfg *SemanticConfig) (*EmbeddingMatcher, error) {
	em := &EmbeddingMatcher{
		client:     client,
		threshold:  cfg.Threshold,
		ambiguous:  cfg.AmbiguousThreshold,
		comparison: cfg.Comparison,
	}

	if em.comparison == "" {
		em.comparison = "centroid"
	}

	for _, route := range routes {
		if len(route.Examples) == 0 {
			continue
		}

		vectors, err := client.EmbedBatch(ctx, route.Examples)
		if err != nil {
			return nil, fmt.Errorf("failed to embed exemplars for route '%s': %w", route.Name, err)
		}

		re := routeEmbeddings{
			name:      route.Name,
			exemplars: vectors,
			centroid:  centroid(vectors),
		}
		em.routes = append(em.routes, re)
	}

	return em, nil
}

// maxEmbedChars is the maximum number of characters sent to the embedding model.
// truncating long messages avoids context-length errors while preserving enough
// text for accurate intent classification.
const maxEmbedChars = 2048

// Match embeds the user prompt and returns the best matching route with confidence.
func (em *EmbeddingMatcher) Match(ctx context.Context, info *RequestInfo) (string, float64, error) {
	// extract the last user message for embedding
	prompt := lastUserMessage(info)
	if prompt == "" {
		return "", 0, nil
	}
	if len(prompt) > maxEmbedChars {
		prompt = prompt[:maxEmbedChars]
	}

	vec, err := em.client.Embed(ctx, prompt)
	if err != nil {
		return "", 0, fmt.Errorf("failed to embed prompt: %w", err)
	}

	var bestRoute string
	var bestScore float64

	for _, re := range em.routes {
		score := em.similarity(vec, &re)
		if score > bestScore {
			bestScore = score
			bestRoute = re.name
		}
	}

	return bestRoute, bestScore, nil
}

func (em *EmbeddingMatcher) similarity(vec []float64, re *routeEmbeddings) float64 {
	switch em.comparison {
	case "max":
		var maxSim float64
		for _, ex := range re.exemplars {
			if s := cosine(vec, ex); s > maxSim {
				maxSim = s
			}
		}
		return maxSim

	case "average":
		if len(re.exemplars) == 0 {
			return 0
		}
		var total float64
		for _, ex := range re.exemplars {
			total += cosine(vec, ex)
		}
		return total / float64(len(re.exemplars))

	default: // centroid
		return cosine(vec, re.centroid)
	}
}

func lastUserMessage(info *RequestInfo) string {
	for i := len(info.Messages) - 1; i >= 0; i-- {
		if strings.ToLower(info.Messages[i].Role) == "user" {
			return info.Messages[i].Content
		}
	}
	return ""
}
