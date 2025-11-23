package httpadapter

import (
	"wsgw/test/e2e/app/internal/monitoring"

	"go.opentelemetry.io/otel"
	metric_api "go.opentelemetry.io/otel/metric"
)

type apiHandlerMetrics struct {
	staleWsConnIdCounter metric_api.Int64Counter
}

func newAPIHandlerMetrics() *apiHandlerMetrics {
	meter := otel.Meter(monitoring.OtelScope)
	staleWsConnIdCounter, regErr := meter.Int64Counter(
		"stale.wsconnid.counter",
		metric_api.WithDescription("Number of stale websocket connection-ids encountered"),
		metric_api.WithUnit("{call}"),
	)

	if regErr != nil {
		panic(regErr)
	}

	return &apiHandlerMetrics{
		staleWsConnIdCounter,
	}
}
