package providers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/michaelquigley/df/dl"
)

// endpoint wraps a single backend instance with health state.
type endpoint struct {
	name             string
	local            *Local
	healthy          bool
	consecutiveFails int
	nextCheck        time.Time
	mu               sync.RWMutex
}

func (e *endpoint) isHealthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.healthy
}

func (e *endpoint) setHealthy(h bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.healthy = h
}

// EndpointOption configures a single endpoint for MultiLocal.
type EndpointOption struct {
	Name       string
	BaseURL    string
	HTTPClient *http.Client // nil for default
	Weight     int          // round-robin weight (default: 1)
}

// MultiLocal distributes requests across multiple endpoints
// using weighted round-robin with health-check-based failover.
type MultiLocal struct {
	endpoints       []*endpoint // weighted: endpoint with weight N appears N times
	uniqueEndpoints []*endpoint // deduplicated: one entry per physical endpoint
	counter         atomic.Uint64
	interval        time.Duration // base health check interval (for backoff calculation)
	lastCheckAt     time.Time     // when checkAll last ran (detects VM sleep)
	cancel          context.CancelFunc
	done            chan struct{}
}

// NewMultiLocal creates a MultiLocal from the given endpoint options.
func NewMultiLocal(opts []EndpointOption) *MultiLocal {
	var uniqueEndpoints []*endpoint
	var endpoints []*endpoint

	for _, opt := range opts {
		var l *Local
		if opt.HTTPClient != nil {
			l = NewLocalWithClient(opt.BaseURL, opt.HTTPClient)
		} else {
			l = NewLocal(opt.BaseURL)
		}
		ep := &endpoint{
			name:    opt.Name,
			local:   l,
			healthy: true,
		}
		uniqueEndpoints = append(uniqueEndpoints, ep)

		weight := opt.Weight
		if weight <= 0 {
			weight = 1
		}
		for j := 0; j < weight; j++ {
			endpoints = append(endpoints, ep)
		}
	}

	return &MultiLocal{
		endpoints:       endpoints,
		uniqueEndpoints: uniqueEndpoints,
		done:            make(chan struct{}),
	}
}

// next returns the next healthy endpoint using round-robin.
// If all endpoints are unhealthy, returns the first endpoint as best-effort.
func (m *MultiLocal) next() *endpoint {
	n := len(m.endpoints)
	start := m.counter.Add(1) - 1
	for i := 0; i < n; i++ {
		ep := m.endpoints[(int(start)+i)%n]
		if ep.isHealthy() {
			return ep
		}
	}
	// all unhealthy — best-effort with the first endpoint
	return m.uniqueEndpoints[0]
}

// isNetworkError returns true for errors that indicate the endpoint is down,
// not application-level errors.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// check for net errors (connection refused, timeout, DNS, etc.)
	if _, ok := err.(net.Error); ok {
		return true
	}
	// unwrap and check inner error
	if uw, ok := err.(interface{ Unwrap() error }); ok {
		return isNetworkError(uw.Unwrap())
	}
	return false
}

func (m *MultiLocal) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	var lastErr error
	for i := 0; i < len(m.uniqueEndpoints); i++ {
		ep := m.next()
		resp, err := ep.local.ChatCompletion(ctx, req)
		if err == nil {
			return resp, nil
		}
		if isNetworkError(err) {
			dl.Errorf("endpoint '%s' network error: %v", ep.name, err)
			ep.setHealthy(false)
			lastErr = err
			continue
		}
		// application-level error — don't failover
		return nil, err
	}
	return nil, fmt.Errorf("all endpoints failed, last error: %w", lastErr)
}

func (m *MultiLocal) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan StreamEvent, error) {
	var lastErr error
	for i := 0; i < len(m.uniqueEndpoints); i++ {
		ep := m.next()
		events, err := ep.local.ChatCompletionStream(ctx, req)
		if err == nil {
			return events, nil
		}
		if isNetworkError(err) {
			dl.Errorf("endpoint '%s' network error: %v", ep.name, err)
			ep.setHealthy(false)
			lastErr = err
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("all endpoints failed, last error: %w", lastErr)
}

// ListModels returns the union of models from all healthy endpoints, deduplicated by model ID.
func (m *MultiLocal) ListModels(ctx context.Context) ([]Model, error) {
	seen := make(map[string]struct{})
	var models []Model
	var lastErr error

	for _, ep := range m.uniqueEndpoints {
		if !ep.isHealthy() {
			continue
		}
		epModels, err := ep.local.ListModels(ctx)
		if err != nil {
			dl.Errorf("endpoint '%s' list models error: %v", ep.name, err)
			lastErr = err
			continue
		}
		for _, model := range epModels {
			if _, ok := seen[model.ID]; !ok {
				seen[model.ID] = struct{}{}
				models = append(models, model)
			}
		}
	}

	if len(models) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return models, nil
}

// StartHealthChecks begins periodic health checking of all endpoints.
// Failing endpoints are rechecked with exponential backoff (up to 10x the
// base interval) to avoid hammering infrastructure that is rate-limiting.
func (m *MultiLocal) StartHealthChecks(interval, timeout time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.interval = interval

	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// run an initial check immediately
		m.checkAll(timeout)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkAll(timeout)
			}
		}
	}()
}

const (
	maxBackoffMultiplier = 10             // cap backoff at 10x the base interval
	staggerDelay         = 5 * time.Second // delay between endpoint checks after VM wake
)

func (m *MultiLocal) checkAll(timeout time.Duration) {
	now := time.Now()

	// detect VM sleep: if elapsed time since last check is more than 2x the
	// interval, the system likely slept and all zrok sessions are stale.
	// stagger the checks to avoid flooding the controller with re-auth requests.
	stagger := !m.lastCheckAt.IsZero() && now.Sub(m.lastCheckAt) > m.interval*2
	if stagger {
		dl.Infof("detected long gap since last health check (%s), staggering endpoint checks", now.Sub(m.lastCheckAt).Round(time.Second))
	}
	m.lastCheckAt = now

	for i, ep := range m.uniqueEndpoints {
		if stagger && i > 0 {
			time.Sleep(staggerDelay)
		}

		ep.mu.RLock()
		skip := now.Before(ep.nextCheck)
		ep.mu.RUnlock()
		if skip {
			continue
		}

		wasHealthy := ep.isHealthy()
		healthy := m.checkEndpoint(ep, timeout)
		ep.setHealthy(healthy)

		ep.mu.Lock()
		if healthy {
			ep.consecutiveFails = 0
			ep.nextCheck = time.Time{}
		} else {
			ep.consecutiveFails++
			backoff := min(ep.consecutiveFails, maxBackoffMultiplier)
			ep.nextCheck = now.Add(time.Duration(backoff) * m.interval)
		}
		ep.mu.Unlock()

		if wasHealthy && !healthy {
			dl.Infof("endpoint '%s' is now unhealthy", ep.name)
		} else if !wasHealthy && healthy {
			dl.Infof("endpoint '%s' is now healthy", ep.name)
		}
	}
}

func (m *MultiLocal) checkEndpoint(ep *endpoint, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// try the standard OpenAI-compatible endpoint first, so non-Ollama backends
	// (vLLM, llama-server, SGLang, etc.) pass health checks
	if m.probe(ctx, ep, "/v1/models") {
		return true
	}

	// fall back to Ollama's native endpoint
	return m.probe(ctx, ep, "/api/tags")
}

func (m *MultiLocal) probe(ctx context.Context, ep *endpoint, path string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", ep.local.baseURL+path, nil)
	if err != nil {
		return false
	}

	resp, err := ep.local.client.Do(req)
	if err != nil {
		dl.Errorf("health check '%s' (%s) failed: %v", ep.name, path, err)
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// PrimaryBaseURL returns the first endpoint's base URL (for embedding provider).
func (m *MultiLocal) PrimaryBaseURL() string {
	return m.uniqueEndpoints[0].local.baseURL
}

// RoundRobinClient returns an HTTP client that distributes requests across
// healthy endpoints with failover. It rewrites request URLs to target the
// selected endpoint and uses that endpoint's transport (supporting zrok).
func (m *MultiLocal) RoundRobinClient() *http.Client {
	return &http.Client{
		Transport: &roundRobinTransport{
			endpoints: m.endpoints,
			counter:   &m.counter,
		},
	}
}

// roundRobinTransport implements http.RoundTripper, distributing requests
// across multiple endpoints with health-aware failover.
type roundRobinTransport struct {
	endpoints []*endpoint
	counter   *atomic.Uint64
}

func (t *roundRobinTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := len(t.endpoints)
	start := t.counter.Add(1) - 1

	var lastErr error
	tried := 0
	for i := 0; i < n; i++ {
		ep := t.endpoints[(int(start)+i)%n]
		if !ep.isHealthy() {
			continue
		}
		tried++

		resp, err := t.doWithEndpoint(ep, req)
		if err == nil {
			return resp, nil
		}
		if isNetworkError(err) {
			dl.Errorf("endpoint '%s' network error: %v", ep.name, err)
			ep.setHealthy(false)
			lastErr = err
			continue
		}
		return nil, err
	}

	// all unhealthy — best-effort with the first endpoint
	if tried == 0 {
		return t.doWithEndpoint(t.endpoints[0], req)
	}
	return nil, fmt.Errorf("all endpoints failed, last error: %w", lastErr)
}

func (t *roundRobinTransport) doWithEndpoint(ep *endpoint, origReq *http.Request) (*http.Response, error) {
	epURL, err := url.Parse(ep.local.baseURL)
	if err != nil {
		return nil, fmt.Errorf("bad endpoint URL: %w", err)
	}

	req := origReq.Clone(origReq.Context())
	req.URL.Scheme = epURL.Scheme
	req.URL.Host = epURL.Host
	req.Host = epURL.Host

	transport := ep.local.client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(req)
}

// Close stops health checks and releases resources.
func (m *MultiLocal) Close() error {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	return nil
}
