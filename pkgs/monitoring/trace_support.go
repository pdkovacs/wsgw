package monitoring

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/propagation"
)

func InjectTraceData(ctx context.Context) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}
	propagator := propagation.TraceContext{}
	propagator.Inject(ctx, carrier)
	return carrier
}

func ExtractTraceData(ctx context.Context, data map[string]string) context.Context {
	carrier := propagation.MapCarrier{}
	for key, value := range data {
		fmt.Printf("key: %s, value: %s\n", key, value)
		carrier.Set(key, value)
	}
	propagator := propagation.TraceContext{}
	return propagator.Extract(ctx, carrier)
}

func InjectIntoHeader(ctx context.Context, headers http.Header) http.Header {
	propagator := propagation.TraceContext{}
	propagator.Inject(ctx, propagation.HeaderCarrier(headers))
	return headers
}

func ExtractFromHeader(ctx context.Context, headers http.Header) context.Context {
	propagator := propagation.TraceContext{}
	return propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}
