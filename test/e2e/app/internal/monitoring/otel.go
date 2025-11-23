package monitoring

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/config"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
)

const OtelScope = "github.com/pdkovacs/wsgw/test/e2e/app"

func InitOtel(ctx context.Context, conf config.Config) {
	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "InitOtel").Str("OtlpEndpoint", conf.OtlpEndpoint).Logger()

	logger.Info().Msg("Otel init starting...")

	endpoint, err := url.Parse(conf.OtlpEndpoint)
	if err != nil {
		logger.Error().Err(err).Msg("failed to parse OTLP endpoint url")
		panic(fmt.Sprintf("failed to parse endpoint url %s: %v", conf.OtlpEndpoint, err))
	}
	insecure := endpoint.Scheme == "http"
	// protocol := "http/protobuf"

	var exporter sdkmetric.Exporter

	if insecure {
		exporter, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint.Host), otlpmetrichttp.WithInsecure())
	} else {
		exporter, err = otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpoint(endpoint.Host))
	}
	if err != nil {
		logger.Error().Err(err).Msg("failed to create exporter")
		panic(fmt.Sprintf("failed to create exporter: %v", err))
	}

	serviceComponent := "main"
	serviceNamespace := conf.OtlpServiceNamespace
	servcieName := conf.OtlpServiceName
	serviceInstanceID := conf.OtlpServiceInstanceId
	if len(serviceInstanceID) == 0 {
		if serviceInstanceID, err = os.Hostname(); err != nil {
			logger.Error().Err(err).Msg("failed to query hostname")
			panic(fmt.Sprintf("failed to query hostname: %v\n", err))
		}
	}

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
	metricReader := sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(5*time.Second))

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(provider)
	addBuiltInGoMetricsToOTEL()
}
