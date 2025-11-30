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
		"api.message.request.counter",
		"Number of message sending requests via api",
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

type wsHandlerMetrics struct {
	connectRequestCounter    metric_api.Int64Counter
	disconnectRequestCounter metric_api.Int64Counter
}

func newWSHandlerMetrics() *wsHandlerMetrics {

	connectRequestCounter := createCounter(
		"ws.connect.request.counter",
		"Number of websocket connection requests",
	)

	disconnectRequestCounter := createCounter(
		"ws.disconnect.request.counter",
		"Number of websocket disconnection requests",
	)

	return &wsHandlerMetrics{
		connectRequestCounter:    connectRequestCounter,
		disconnectRequestCounter: disconnectRequestCounter,
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
