package internal

import (
	"context"
	"time"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/client/internal/config"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type clientMonitoring struct {
	incMsgRecipientToReachCounter    func(ctx context.Context, count int64)
	incMsgCreatedCounter             func(ctx context.Context)
	incMsgParseErrCounter            func(ctx context.Context)
	incOutstandingMsgNotFoundCounter func(ctx context.Context)
	incdMsgTextMismatchCounter       func(ctx context.Context)
	recordDeliveryDurationMs         func(ctx context.Context, deliveryTime time.Duration)
	recordDeliveryDurationUs         func(ctx context.Context, deliveryTime time.Duration)
}

func createMetrics(runId string) *clientMonitoring {
	incMsgRecipientToReachCounter := createCounter("ws.e2e.test.client.msg.recipient.counter", "MsgRecipientToReachCounter")
	incMsgCreatedCounter := createCounter("ws.e2e.test.client.msg.created", "MsgCreatedCounter")
	incMsgParseErrCounter := createCounter("ws.e2e.test.client.msg.parse.error", "MsgParseErrCounter")
	incMsgNotFoundCounter := createCounter("ws.e2e.test.client.outstanding.msg.notfound.error", "MsgNotFoundCounter")
	incdMsgTextMismatchCounter := createCounter("ws.e2e.test.client.outstanding.msg.text.mismatch.error", "dMsgTextMismatchCounter")

	deliveryDurationHistogramMs := monitoring.CreateHistogram(
		config.OtelScope,
		"ws.e2e.test.delivery.duration.ms",
		"Message delivery duration in milliseconds",
		"ms",
		metric.WithExplicitBucketBoundaries(0, 1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 4000, 8000, 16000),
	)
	deliveryDurationHistogramUs := monitoring.CreateHistogram(config.OtelScope, "ws.e2e.test.delivery.duration.us", "Message delivery duration in microseconds", "us")

	attributes := metric.WithAttributes(attribute.KeyValue{Key: "runId", Value: attribute.StringValue(runId)})

	return &clientMonitoring{
		incMsgRecipientToReachCounter: func(ctx context.Context, count int64) {
			incMsgRecipientToReachCounter.Add(ctx, count, attributes)
		},
		incMsgCreatedCounter: func(ctx context.Context) {
			incMsgCreatedCounter.Add(ctx, 1, attributes)
		},
		incMsgParseErrCounter: func(ctx context.Context) {
			incMsgParseErrCounter.Add(ctx, 1, attributes)
		},
		incOutstandingMsgNotFoundCounter: func(ctx context.Context) {
			incMsgNotFoundCounter.Add(ctx, 1, attributes)
		},
		incdMsgTextMismatchCounter: func(ctx context.Context) {
			incdMsgTextMismatchCounter.Add(ctx, 1, attributes)
		},
		recordDeliveryDurationMs: func(ctx context.Context, deliveryDuration time.Duration) {
			deliveryDurationHistogramMs.Record(ctx, float64(deliveryDuration.Microseconds())/1000.0, attributes)
		},
		recordDeliveryDurationUs: func(ctx context.Context, deliveryDuration time.Duration) {
			deliveryDurationHistogramUs.Record(ctx, float64(deliveryDuration.Microseconds()), attributes)
		},
	}
}

func createCounter(name string, desc string) metric.Int64Counter {
	return monitoring.CreateCounter(config.OtelScope, name, desc)
}
