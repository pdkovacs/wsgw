package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/pgks/dto"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	numCoroutines int = 16
)

var transport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConnsPerHost:   numCoroutines,
	ResponseHeaderTimeout: 90 * time.Second,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 10 * time.Second,
}

var httpClient http.Client = http.Client{
	Transport: transport,
	Timeout:   90 * time.Second,
}

func SendMessage(ctx context.Context, wsgwUrl string, userId string, message *dto.E2EMessage, wsConnIds []string, discardConnId func(connId string)) error {
	logger0 := zerolog.Ctx(ctx).With().Str("wsgwUrl", wsgwUrl).Logger()
	logger0.Debug().Str("recipient", userId).Any("msg", message).Int("targetConnectionCount", len(wsConnIds)).Msg("message to send...")

	var err error

	tracer := otel.Tracer(config.OtelScope)
	userCtx, userSpan := tracer.Start(
		ctx,
		"send-message",
		trace.WithAttributes(attribute.String("runId", message.TestRunId)),
	)
	defer userSpan.End()

	for _, connId := range wsConnIds {
		url := fmt.Sprintf("%s%s/%s", wsgwUrl, wsgw.MessagePath, connId)
		logger := logger0.With().Str("url", url).Logger()

		deviceCtx, deviceSpan := tracer.Start(
			userCtx,
			"send-message-to-userdevice",
			trace.WithAttributes(
				attribute.String("runId", message.TestRunId),
				attribute.String("msgId", message.Id),
				attribute.String("destination", message.Destination),
			),
		)

		// "Multiplex" a single message
		message.Destination = connId
		message.SetSentAt()
		message.TraceData = monitoring.InjectTraceData(deviceCtx)

		messageAsString, marshalErr := json.Marshal(message)
		if marshalErr != nil {
			deviceSpan.RecordError(fmt.Errorf("while marshalling message: %w", marshalErr))
			deviceSpan.SetStatus(codes.Error, "failed to marshal message")
			deviceSpan.End()

			logger.Error().Err(marshalErr).Any("messageIn", message).Msg("failed to marshal message")

			continue
		}

		logger.Debug().Msg("address to send to...")
		req, createReqErr := http.NewRequestWithContext(deviceCtx, http.MethodPost, url, strings.NewReader(string(messageAsString)))
		if createReqErr != nil {
			logger.Error().Err(createReqErr).Msg("failed to create request")

			deviceSpan.RecordError(fmt.Errorf("while creating request: %w", createReqErr))
			deviceSpan.SetStatus(codes.Error, "failed to create request")
			deviceSpan.End()

			continue
		}

		monitoring.InjectIntoHeader(deviceCtx, req.Header)

		deviceSpan.AddEvent("sending-request")

		response, sendReqErr := httpClient.Do(req)
		if sendReqErr != nil {
			logger.Error().Err(sendReqErr).Msg("failed to send request")
			err = errors.Join(err, sendReqErr)

			deviceSpan.RecordError(fmt.Errorf("while sending request: %w", sendReqErr))
			deviceSpan.SetStatus(codes.Error, "failed to send request")
			deviceSpan.End()

			continue
		}
		defer func() {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}()

		if response.StatusCode != http.StatusNoContent {
			if response.StatusCode == http.StatusNotFound {
				discardConnId(connId)
				logger.Debug().Str("connId", connId).Msg("404: discarding ws connection reference")
				deviceSpan.End()

				continue
			}

			logger.Error().Str("url", url).Msg("failed to send request")
			err = errors.Join(err, fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode))

			deviceSpan.RecordError(fmt.Errorf("unexpected status: %v", response.StatusCode))
			deviceSpan.SetStatus(codes.Error, "failed to send request")
			deviceSpan.End()

			continue
		}
		deviceSpan.AddEvent("request-sent")

		deviceSpan.SetStatus(codes.Ok, "message sent to wsgw")
		deviceSpan.End()

		logger.Debug().
			Bool("ctxCancelledAfterEnd", deviceCtx.Err() != nil).
			Msg("context status after span end")
	}

	return err
}
