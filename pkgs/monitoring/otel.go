package monitoring

import (
	"context"
	"fmt"
	"net/url"
	"time"
	"wsgw/internal/config"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metric_api "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
)

type OtelConfig struct {
	OtlpEndpoint         string
	OtlpServiceNamespace string
	OtlpServiceName      string
	OtlpTraceSampleAll   bool
}

func InitOtel(ctx context.Context, conf OtelConfig, otelScope string) {
	logger := zerolog.Ctx(ctx).With().Str("OtlpEndpoint", conf.OtlpEndpoint).Logger()

	if len(conf.OtlpEndpoint) == 0 {
		logger.Info().Msg("No OTLP endpoint, skipping...")
		return
	}

	logger.Info().Msg("starting...")

	endpoint, err := url.Parse(conf.OtlpEndpoint)
	if err != nil {
		logger.Error().Err(err).Msg("failed to parse OTLP endpoint url")
		panic(fmt.Sprintf("failed to parse endpoint url %s: %v", conf.OtlpEndpoint, err))
	}
	insecure := endpoint.Scheme == "http"
	// protocol := "http/protobuf"

	var metricExporter sdkmetric.Exporter

	if insecure {
		metricExporter, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint.Host), otlpmetrichttp.WithInsecure())
	} else {
		metricExporter, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint.Host))
	}
	if err != nil {
		logger.Error().Err(err).Msg("failed to create exporter")
		panic(fmt.Sprintf("failed to create exporter: %v", err))
	}

	serviceComponent := "main"
	serviceNamespace := conf.OtlpServiceNamespace
	servcieName := conf.OtlpServiceName
	serviceInstanceID := config.GetInstanceId()

	// See also https://github.com/pdkovacs/forked-quickpizza/commit/a5835b3b84d4ae995b8b886a6982a59f3997af2e
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(servcieName),
			attribute.KeyValue{Key: "service.component", Value: attribute.StringValue(serviceComponent)},
			attribute.KeyValue{Key: "service.namespace", Value: attribute.StringValue(serviceNamespace)},
			attribute.KeyValue{Key: "service.instance.id", Value: attribute.StringValue(serviceInstanceID)},
		),
	)
	metricReader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(5*time.Second))

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(provider)
	addBuiltInGoMetricsToOTEL(otelScope)

	traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint.Host), otlptracehttp.WithInsecure())
	if err != nil {
		panic(err)
	}

	traceOptions := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	}
	if conf.OtlpTraceSampleAll {
		traceOptions = append(traceOptions, trace.WithSampler(trace.AlwaysSample()))
	}
	tp := sdktrace.NewTracerProvider(
		traceOptions...,
	)
	otel.SetTracerProvider(tp)
}

func CreateCounter(otelScope string, name string, description string, options ...metric_api.Int64CounterOption) metric_api.Int64Counter {
	meter := otel.Meter(otelScope)

	options = append(options, metric_api.WithDescription(description))
	options = append(options, metric_api.WithUnit("{call}"))

	counter, regErr := meter.Int64Counter(name, options...)

	if regErr != nil {
		panic(regErr)
	}

	return counter
}

func CreateHistogram(otelScope string, name string, description string, unit string, options ...metric_api.Float64HistogramOption) metric_api.Float64Histogram {
	options = append(options, metric_api.WithDescription(description))
	options = append(options, metric_api.WithUnit("{call}"))

	histogram, err := otel.Meter(otelScope).Float64Histogram(name, options...)
	if err != nil {
		panic(err)
	}

	return histogram
}

func CreateGague(otelScope string, name string, description string, unit string) metric_api.Int64Gauge {
	meter := otel.Meter(otelScope)

	speedGauge, err := meter.Int64Gauge(
		name,
		metric_api.WithDescription(description),
		metric_api.WithUnit(unit),
	)
	if err != nil {
		panic(err)
	}

	return speedGauge
}

func CreateUpDownCounter(otelScope string, name string, description string, unit string) metric_api.Int64UpDownCounter {
	meter := otel.Meter(otelScope)

	speedGauge, err := meter.Int64UpDownCounter(
		name,
		metric_api.WithDescription(description),
		metric_api.WithUnit(unit),
	)
	if err != nil {
		panic(err)
	}

	return speedGauge
}

func TrackInFlight(ctx context.Context, inflightGauge metric_api.Int64UpDownCounter, spanName string, fn func(context.Context) error) error {
	inflightGauge.Add(ctx, 1, metric.WithAttributes(
		attribute.String("span.name", spanName),
	))
	defer inflightGauge.Add(ctx, -1, metric.WithAttributes(
		attribute.String("span.name", spanName),
	))

	return fn(ctx)
}
