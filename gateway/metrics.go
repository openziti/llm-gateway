package gateway

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const meterName = "llm-gateway"

// meters holds all OTel metric instruments for the gateway.
type meters struct {
	requests         metric.Int64Counter
	requestDuration  metric.Float64Histogram
	tokensPrompt     metric.Int64Counter
	tokensCompletion metric.Int64Counter
	routingDecisions metric.Int64Counter
	providerErrors   metric.Int64Counter
	inflight         metric.Int64UpDownCounter
	endpointHealthy  metric.Int64UpDownCounter
}

// initMetrics sets up the OTel meter provider with Prometheus exporter
// and creates all metric instruments.
func initMetrics() (*meters, http.Handler, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, err
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)

	meter := provider.Meter(meterName)
	m := &meters{}

	m.requests, err = meter.Int64Counter("llm_gateway.requests",
		metric.WithDescription("total chat completion requests"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.requestDuration, err = meter.Float64Histogram("llm_gateway.request.duration",
		metric.WithDescription("request duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.tokensPrompt, err = meter.Int64Counter("llm_gateway.tokens.prompt",
		metric.WithDescription("total prompt tokens"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.tokensCompletion, err = meter.Int64Counter("llm_gateway.tokens.completion",
		metric.WithDescription("total completion tokens"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.routingDecisions, err = meter.Int64Counter("llm_gateway.routing.decisions",
		metric.WithDescription("semantic routing decisions"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.providerErrors, err = meter.Int64Counter("llm_gateway.provider.errors",
		metric.WithDescription("provider errors"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.inflight, err = meter.Int64UpDownCounter("llm_gateway.requests.inflight",
		metric.WithDescription("currently in-flight requests"),
	)
	if err != nil {
		return nil, nil, err
	}

	m.endpointHealthy, err = meter.Int64UpDownCounter("llm_gateway.endpoint.healthy",
		metric.WithDescription("endpoint health status (1=healthy, 0=unhealthy)"),
	)
	if err != nil {
		return nil, nil, err
	}

	return m, promhttp.Handler(), nil
}
