package routing

// RoutingConfig is the top-level semantic routing configuration.
type RoutingConfig struct {
	AllowExplicitModel *bool
	DefaultRoute       string
	Heuristics         *HeuristicsConfig
	Semantic           *SemanticConfig
	Classifier         *ClassifierConfig
	Routes             []RouteConfig
}

// AllowExplicit returns whether explicit model selection is allowed.
// defaults to true if not set.
func (c *RoutingConfig) AllowExplicit() bool {
	if c.AllowExplicitModel == nil {
		return true
	}
	return *c.AllowExplicitModel
}

// HeuristicsConfig configures heuristic-based routing rules.
type HeuristicsConfig struct {
	Enabled bool
	Rules   []HeuristicRule
}

// HeuristicRule defines a single heuristic routing rule.
type HeuristicRule struct {
	Match MatchCondition
	Route string
}

// MatchCondition defines conditions for a heuristic rule.
// all non-zero fields must match (AND logic).
type MatchCondition struct {
	Keywords             []string
	Exclude              []string // phrases that suppress a keyword match
	SystemPromptContains string
	MaxTokensLt          *int
	MessageLengthLt      *int
	HasTools             *bool
}

// SemanticConfig configures embedding-based semantic matching.
type SemanticConfig struct {
	Enabled            bool
	Provider           string // local or openai
	Model              string
	Threshold          float64
	AmbiguousThreshold float64
	Comparison         string // centroid, max, or average
	CacheEmbeddings    bool
	CacheTTL           int // seconds, default 3600
	CacheSize          int // max entries, default 1000
}

// ClassifierConfig configures LLM-based classification.
type ClassifierConfig struct {
	Enabled             bool
	Provider            string // local or openai
	Model               string
	TimeoutMs           int
	ConfidenceThreshold float64
	CacheResults        bool
	CacheTTL            int // seconds, default 3600
	CacheSize           int // max entries, default 500
}

// RouteConfig defines a named route with a target model and exemplars.
type RouteConfig struct {
	Name        string
	Model       string
	Description string
	Examples    []string
}
