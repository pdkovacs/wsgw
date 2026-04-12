package monitoring

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/propagation"
)

func InjectTraceData(ctx context.Context) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}
	propagator := propagation.TraceContext{}
	propagator.Inject(ctx, carrier)
	return carrier
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
