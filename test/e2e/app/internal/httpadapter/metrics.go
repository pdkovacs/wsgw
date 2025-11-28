package httpadapter

import (
	"wsgw/test/e2e/app/internal/monitoring"

	"go.opentelemetry.io/otel"
	metric_api "go.opentelemetry.io/otel/metric"
)

type apiHandlerMetrics struct {
	messageRequestCounter metric_api.Int64Counter
	staleWsConnIdCounter  metric_api.Int64Counter
}

func newAPIHandlerMetrics() *apiHandlerMetrics {

	messageRequestCounter := createCounter(
		"message.api.request.counter",
		"Number of message sending request via api",
	)

	staleWsConnIdCounter := createCounter(
		"stale.wsconnid.counter",
		"Number of stale websocket connection-ids encountered",
	)

	return &apiHandlerMetrics{
		messageRequestCounter: messageRequestCounter,
		staleWsConnIdCounter:  staleWsConnIdCounter,
	}
}

func createCounter(name string, description string) metric_api.Int64Counter {
	meter := otel.Meter(monitoring.OtelScope)

	counter, regErr := meter.Int64Counter(
		name,
		metric_api.WithDescription(description),
		metric_api.WithUnit("{call}"),
	)

	if regErr != nil {
		panic(regErr)
	}

	return counter
}
