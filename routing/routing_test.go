package routing

import (
	"context"
	"testing"
)

func TestSemanticRouterExplicitModel(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Routes: []RouteConfig{
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Model:    "gpt-4",
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Method != MethodExplicit {
		t.Errorf("expected explicit method, got %s", decision.Method)
	}
	if decision.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got '%s'", decision.Model)
	}
}

func TestSemanticRouterExplicitModelDisallowed(t *testing.T) {
	allow := false
	cfg := &RoutingConfig{
		AllowExplicitModel: &allow,
		DefaultRoute:       "general",
		Routes: []RouteConfig{
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Model:    "gpt-4",
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// should not use explicit method since it's disallowed
	if decision.Method == MethodExplicit {
		t.Error("explicit model should be disallowed")
	}
}

func TestSemanticRouterHeuristic(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Heuristics: &HeuristicsConfig{
			Enabled: true,
			Rules: []HeuristicRule{
				{
					Match: MatchCondition{Keywords: []string{"translate"}},
					Route: "fast",
				},
			},
		},
		Routes: []RouteConfig{
			{Name: "fast", Model: "llama3"},
			{Name: "general", Model: "gpt-4"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "translate this to French"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Method != MethodHeuristic {
		t.Errorf("expected heuristic method, got %s", decision.Method)
	}
	if decision.Model != "llama3" {
		t.Errorf("expected 'llama3', got '%s'", decision.Model)
	}
}

func TestSemanticRouterSemantic(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float64{
			"write code":    {1, 0},
			"fix bugs":      {0.9, 0.1},
			"write a poem":  {0, 1},
			"tell a story":  {0.1, 0.9},
			"debug my code": {0.95, 0.05},
		},
	}

	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Semantic: &SemanticConfig{
			Enabled:            true,
			Threshold:          0.8,
			AmbiguousThreshold: 0.5,
			Comparison:         "centroid",
		},
		Routes: []RouteConfig{
			{Name: "coding", Model: "gpt-4", Examples: []string{"write code", "fix bugs"}},
			{Name: "creative", Model: "claude-3", Examples: []string{"write a poem", "tell a story"}},
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "debug my code"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Method != MethodSemantic {
		t.Errorf("expected semantic method, got %s", decision.Method)
	}
	if decision.Route != "coding" {
		t.Errorf("expected 'coding' route, got '%s'", decision.Route)
	}
}

func TestSemanticRouterDefault(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Routes: []RouteConfig{
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Method != MethodDefault {
		t.Errorf("expected default method, got %s", decision.Method)
	}
	if decision.Model != "llama3" {
		t.Errorf("expected 'llama3', got '%s'", decision.Model)
	}
}

func TestSemanticRouterCascadeOrder(t *testing.T) {
	embedder := &mockEmbedder{
		vectors: map[string][]float64{
			"example":    {1, 0},
			"test input": {0.5, 0.5}, // not close enough to any route
		},
	}

	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Heuristics: &HeuristicsConfig{
			Enabled: true,
			Rules:   []HeuristicRule{}, // no rules that match
		},
		Semantic: &SemanticConfig{
			Enabled:            true,
			Threshold:          0.95, // high threshold, embedding won't meet it
			AmbiguousThreshold: 0.3,
			Comparison:         "centroid",
		},
		Routes: []RouteConfig{
			{Name: "specific", Model: "gpt-4", Examples: []string{"example"}},
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, embedder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "test input"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// should fall through all layers to default
	if decision.Method != MethodDefault {
		t.Errorf("expected default method, got %s", decision.Method)
	}

	// cascade should show all attempted layers with detail
	if len(decision.Cascade) < 3 {
		t.Errorf("expected at least 3 cascade entries, got %v", decision.Cascade)
	}
	if decision.Cascade[0] != "heuristic:no_match" {
		t.Errorf("expected cascade[0] = 'heuristic:no_match', got %q", decision.Cascade[0])
	}
	// semantic entry should start with "semantic:"
	if len(decision.Cascade) > 1 {
		if decision.Cascade[1][:9] != "semantic:" {
			t.Errorf("expected cascade[1] to start with 'semantic:', got %q", decision.Cascade[1])
		}
	}
	// last entry should start with "default:"
	last := decision.Cascade[len(decision.Cascade)-1]
	if last[:8] != "default:" {
		t.Errorf("expected last cascade entry to start with 'default:', got %q", last)
	}
}

func TestSemanticRouterHeuristicCascadeFormat(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Heuristics: &HeuristicsConfig{
			Enabled: true,
			Rules: []HeuristicRule{
				{
					Match: MatchCondition{Keywords: []string{"translate"}},
					Route: "fast",
				},
			},
		},
		Routes: []RouteConfig{
			{Name: "fast", Model: "llama3"},
			{Name: "general", Model: "gpt-4"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "translate this to French"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decision.Cascade) != 1 || decision.Cascade[0] != "heuristic:fast" {
		t.Errorf("expected cascade ['heuristic:fast'], got %v", decision.Cascade)
	}
}

func TestSemanticRouterExplicitCascadeFormat(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Routes: []RouteConfig{
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Model:    "gpt-4",
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decision.Cascade) != 1 || decision.Cascade[0] != "explicit:gpt-4" {
		t.Errorf("expected cascade ['explicit:gpt-4'], got %v", decision.Cascade)
	}
}

func TestSemanticRouterDefaultCascadeFormat(t *testing.T) {
	cfg := &RoutingConfig{
		DefaultRoute: "general",
		Routes: []RouteConfig{
			{Name: "general", Model: "llama3"},
		},
	}

	sr, err := NewSemanticRouter(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := &RequestInfo{
		Messages: []MessageInfo{{Role: "user", Content: "hello"}},
	}

	decision, err := sr.Route(context.Background(), info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decision.Cascade) != 1 || decision.Cascade[0] != "default:general" {
		t.Errorf("expected cascade ['default:general'], got %v", decision.Cascade)
	}
}

func TestSemanticRouterEnabled(t *testing.T) {
	sr := &SemanticRouter{cfg: &RoutingConfig{}}
	if !sr.Enabled() {
		t.Error("expected enabled with config")
	}

	var nilRouter *SemanticRouter
	if nilRouter.Enabled() {
		t.Error("expected disabled for nil router")
	}
}
