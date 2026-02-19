package routing

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/michaelquigley/df/dl"
)

// Method identifies how a routing decision was made.
type Method string

const (
	MethodExplicit   Method = "explicit"
	MethodHeuristic  Method = "heuristic"
	MethodSemantic   Method = "semantic"
	MethodClassifier Method = "classifier"
	MethodDefault    Method = "default"
)

// Decision describes the result of a routing decision.
type Decision struct {
	Route      string
	Model      string
	Method     Method
	Confidence float64
	LatencyMs  int64
	Cascade    []string
}

// RequestInfo is a provider-independent representation of a chat request.
type RequestInfo struct {
	Model     string
	Messages  []MessageInfo
	MaxTokens *int
	HasTools  bool
}

// MessageInfo is a simplified message for routing decisions.
type MessageInfo struct {
	Role    string
	Content string
}

// Embedder is the interface for generating text embeddings.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}

// SemanticRouter orchestrates the three-layer routing cascade.
type SemanticRouter struct {
	cfg        *RoutingConfig
	heuristics *HeuristicMatcher
	embeddings *EmbeddingMatcher
	classifier *ClassifierMatcher
	routeMap   map[string]RouteConfig
}

// NewSemanticRouter creates a new SemanticRouter, embedding exemplars at startup.
func NewSemanticRouter(ctx context.Context, cfg *RoutingConfig, embedClient Embedder) (*SemanticRouter, error) {
	sr := &SemanticRouter{
		cfg:      cfg,
		routeMap: make(map[string]RouteConfig),
	}

	for _, r := range cfg.Routes {
		sr.routeMap[r.Name] = r
	}

	// layer 1: heuristics
	if cfg.Heuristics != nil && cfg.Heuristics.Enabled {
		sr.heuristics = NewHeuristicMatcher(cfg.Heuristics.Rules)
		dl.Info("initialized heuristic matcher")
	}

	// layer 2: embedding similarity
	if cfg.Semantic != nil && cfg.Semantic.Enabled && embedClient != nil {
		em, err := NewEmbeddingMatcher(ctx, embedClient, cfg.Routes, cfg.Semantic)
		if err != nil {
			return nil, err
		}
		sr.embeddings = em
		dl.Info("initialized embedding matcher")
	}

	// layer 3: llm classifier
	if cfg.Classifier != nil && cfg.Classifier.Enabled {
		sr.classifier = NewClassifierMatcher(cfg.Classifier, cfg.Routes, "", "")
		dl.Info("initialized classifier matcher")
	}

	return sr, nil
}

// NewSemanticRouterWithClassifier creates a SemanticRouter with explicit classifier connection details.
// If httpClient is non-nil it is used for classifier requests (e.g. for zrok transport).
func NewSemanticRouterWithClassifier(ctx context.Context, cfg *RoutingConfig, embedClient Embedder, classifierBaseURL, classifierAPIKey string, httpClient *http.Client) (*SemanticRouter, error) {
	sr, err := NewSemanticRouter(ctx, cfg, embedClient)
	if err != nil {
		return nil, err
	}

	if cfg.Classifier != nil && cfg.Classifier.Enabled {
		if httpClient != nil {
			sr.classifier = NewClassifierMatcherWithHTTPClient(cfg.Classifier, cfg.Routes, classifierBaseURL, classifierAPIKey, httpClient)
		} else {
			sr.classifier = NewClassifierMatcher(cfg.Classifier, cfg.Routes, classifierBaseURL, classifierAPIKey)
		}
		dl.Info("initialized classifier matcher with explicit provider config")
	}

	return sr, nil
}

// Enabled returns true if semantic routing is configured.
func (sr *SemanticRouter) Enabled() bool {
	return sr != nil && sr.cfg != nil
}

// Route performs the routing cascade and returns a decision.
func (sr *SemanticRouter) Route(ctx context.Context, info *RequestInfo) (*Decision, error) {
	start := time.Now()
	var cascade []string

	// explicit model passthrough
	if info.Model != "" && sr.cfg.AllowExplicit() {
		return &Decision{
			Model:      info.Model,
			Method:     MethodExplicit,
			Confidence: 1.0,
			LatencyMs:  time.Since(start).Milliseconds(),
			Cascade:    []string{fmt.Sprintf("explicit:%s", info.Model)},
		}, nil
	}

	// layer 1: heuristics
	if sr.heuristics != nil {
		if route := sr.heuristics.Match(info); route != "" {
			cascade = append(cascade, fmt.Sprintf("heuristic:%s", route))
			if rc, ok := sr.routeMap[route]; ok {
				return &Decision{
					Route:      route,
					Model:      rc.Model,
					Method:     MethodHeuristic,
					Confidence: 1.0,
					LatencyMs:  time.Since(start).Milliseconds(),
					Cascade:    cascade,
				}, nil
			}
		} else {
			cascade = append(cascade, "heuristic:no_match")
		}
	}

	// layer 2: embedding similarity
	if sr.embeddings != nil {
		route, confidence, err := sr.embeddings.Match(ctx, info)
		if err != nil {
			dl.Errorf("embedding match error: %v", err)
		} else if route != "" {
			// check if confident enough
			threshold := sr.cfg.Semantic.Threshold
			ambiguous := sr.cfg.Semantic.AmbiguousThreshold

			if confidence >= threshold {
				cascade = append(cascade, fmt.Sprintf("semantic:%s:%.2f", route, confidence))
				if rc, ok := sr.routeMap[route]; ok {
					return &Decision{
						Route:      route,
						Model:      rc.Model,
						Method:     MethodSemantic,
						Confidence: confidence,
						LatencyMs:  time.Since(start).Milliseconds(),
						Cascade:    cascade,
					}, nil
				}
			}

			// ambiguous: escalate to classifier if available
			if confidence >= ambiguous && sr.classifier != nil {
				cascade = append(cascade, fmt.Sprintf("semantic:%s:%.2f:ambiguous", route, confidence))
				cRoute, cConf, cErr := sr.classifier.Classify(ctx, info)
				if cErr != nil {
					dl.Errorf("classifier error: %v", cErr)
					cascade = append(cascade, "classifier:no_match")
				} else if cRoute != "" && cConf >= sr.cfg.Classifier.ConfidenceThreshold {
					cascade = append(cascade, fmt.Sprintf("classifier:%s:%.2f", cRoute, cConf))
					if rc, ok := sr.routeMap[cRoute]; ok {
						return &Decision{
							Route:      cRoute,
							Model:      rc.Model,
							Method:     MethodClassifier,
							Confidence: cConf,
							LatencyMs:  time.Since(start).Milliseconds(),
							Cascade:    cascade,
						}, nil
					}
				} else {
					cascade = append(cascade, "classifier:no_match")
				}
			} else {
				cascade = append(cascade, "semantic:no_match")
			}
		} else {
			cascade = append(cascade, "semantic:no_match")
		}
	} else if sr.classifier != nil {
		// no embeddings configured, try classifier directly
		route, confidence, err := sr.classifier.Classify(ctx, info)
		if err != nil {
			dl.Errorf("classifier error: %v", err)
			cascade = append(cascade, "classifier:no_match")
		} else if route != "" && confidence >= sr.cfg.Classifier.ConfidenceThreshold {
			cascade = append(cascade, fmt.Sprintf("classifier:%s:%.2f", route, confidence))
			if rc, ok := sr.routeMap[route]; ok {
				return &Decision{
					Route:      route,
					Model:      rc.Model,
					Method:     MethodClassifier,
					Confidence: confidence,
					LatencyMs:  time.Since(start).Milliseconds(),
					Cascade:    cascade,
				}, nil
			}
		} else {
			cascade = append(cascade, "classifier:no_match")
		}
	}

	// default route
	if sr.cfg.DefaultRoute != "" {
		if rc, ok := sr.routeMap[sr.cfg.DefaultRoute]; ok {
			cascade = append(cascade, fmt.Sprintf("default:%s", sr.cfg.DefaultRoute))
			return &Decision{
				Route:      sr.cfg.DefaultRoute,
				Model:      rc.Model,
				Method:     MethodDefault,
				Confidence: 0,
				LatencyMs:  time.Since(start).Milliseconds(),
				Cascade:    cascade,
			}, nil
		}
	}

	// absolute fallback: use first route
	if len(sr.cfg.Routes) > 0 {
		rc := sr.cfg.Routes[0]
		cascade = append(cascade, fmt.Sprintf("default:%s", rc.Name))
		return &Decision{
			Route:      rc.Name,
			Model:      rc.Model,
			Method:     MethodDefault,
			Confidence: 0,
			LatencyMs:  time.Since(start).Milliseconds(),
			Cascade:    cascade,
		}, nil
	}

	cascade = append(cascade, "default")
	return &Decision{
		Method:    MethodDefault,
		LatencyMs: time.Since(start).Milliseconds(),
		Cascade:   cascade,
	}, nil
}
