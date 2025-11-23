package monitoring

import (
	"context"
	"fmt"
	"log"
	"runtime/metrics"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	api "go.opentelemetry.io/otel/metric"
)

const errorValue = -1.0

// addMetricsToPrometheusRegistry function to add metrics to prometheus registry
func addBuiltInGoMetricsToOTEL() {

	meter := otel.Meter(OtelScope)

	// Get descriptions for all supported metrics.
	metricsMeta := metrics.All()

	// Register metrics and retrieve the values in prometheus client
	for i := range metricsMeta {
		// Get metric options
		meta := metricsMeta[i]
		opt := getMetricsOptions(metricsMeta[i])
		name := normalizeOtelName(meta.Name)

		// Register metrics per type of metric
		if meta.Cumulative {
			// Register as a counter
			counter, err := meter.Float64ObservableCounter(name, api.WithDescription(meta.Description))
			if err != nil {
				log.Fatal(err)
			}
			_, err = meter.RegisterCallback(func(_ context.Context, o api.Observer) error {
				o.ObserveFloat64(counter, GetSingleMetricFloat(meta.Name), opt)
				return nil
			}, counter)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			// Register as a gauge
			gauge, err := meter.Float64ObservableGauge(name, api.WithDescription(meta.Description))
			if err != nil {
				log.Fatal(err)
			}
			_, err = meter.RegisterCallback(func(_ context.Context, o api.Observer) error {
				o.ObserveFloat64(gauge, GetSingleMetricFloat(meta.Name), opt)
				return nil
			}, gauge)
			if err != nil {
				log.Fatal(err)
			}
		}
	}
}

// getMetricsOptions function to get metric labels
func getMetricsOptions(metric metrics.Description) api.MeasurementOption {
	tokens := strings.Split(metric.Name, "/")
	if len(tokens) < 2 {
		return nil
	}

	nameTokens := strings.Split(tokens[len(tokens)-1], ":")
	subsystem := GetMetricSubsystemName(metric)

	// create a unique name for metric, that will be its primary key on the registry
	opt := api.WithAttributes(
		attribute.Key("Namespace").String(tokens[1]),
		attribute.Key("Subsystem").String(subsystem),
		attribute.Key("Units").String(nameTokens[1]),
	)
	return opt
}

func normalizeOtelName(name string) string {
	normalizedName := strings.Replace(name, "/", "", 1)
	normalizedName = strings.Replace(normalizedName, ":", "_", -1)
	normalizedName = strings.TrimSpace(strings.ReplaceAll(normalizedName, "/", "_"))
	return normalizedName
}

// Function to get metrics values from runtime/metrics package
func GetAllMetrics() []metrics.Sample {
	metricsMetadata := metrics.All()
	samples := make([]metrics.Sample, len(metricsMetadata))
	// update name of each sample
	for idx := range metricsMetadata {
		samples[idx].Name = metricsMetadata[idx].Name
	}
	metrics.Read(samples)
	return samples
}

// Function to get metrics values from runtime/metrics package as float64
func GetSingleMetricFloat(metricName string) float64 {

	// Create a sample for the metric.
	sample := make([]metrics.Sample, 1)
	sample[0].Name = metricName

	// Sample the metric.
	metrics.Read(sample)

	return getFloat64(sample[0])
}

// function to return differemt sample values as float 64
// curently it handles single values, in future it will handle histograms
func getFloat64(sample metrics.Sample) float64 {
	var floatVal float64
	// Handle each sample.
	switch sample.Value.Kind() {
	case metrics.KindUint64:
		floatVal = float64(sample.Value.Uint64())
	case metrics.KindFloat64:
		floatVal = float64(sample.Value.Float64())
	case metrics.KindFloat64Histogram:
		// TODO: implementation needed
		return errorValue
	case metrics.KindBad:
		panic("bug in runtime/metrics package!")
	default:
		panic(fmt.Sprintf("%s: unexpected metric Kind: %v\n", sample.Name, sample.Value.Kind()))
	}
	return floatVal
}

// Function to get metrics subsysyetm from a mteric metadata
func GetMetricSubsystemName(metric metrics.Description) string {
	tokens := strings.Split(metric.Name, "/")
	if len(tokens) < 2 {
		return ""
	}
	if len(tokens) > 3 {
		subsystemTokens := tokens[2 : len(tokens)-1]
		subsystem := strings.Join(subsystemTokens, "_")
		subsystem = strings.ReplaceAll(subsystem, "-", "_")
		return subsystem
	}
	return ""
}
