package services

import (
	"context"
	"crypto/tls"
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
	"golang.org/x/net/http2"
)

const (
	numCoroutines int = 16
)

// NewWsgwHttpClient returns an HTTP client tuned for sending messages to wsgw.
// When http2Enabled is true the client speaks h2c (HTTP/2 cleartext), multiplexing
// all concurrent sends over a single TCP connection — eliminating the per-host
// connection-count pressure that causes ephemeral-port exhaustion under high load.
// When false it falls back to an HTTP/1.1 client with a connection pool sized to
// the worker concurrency (numCoroutines).
func NewWsgwHttpClient(http2Enabled bool) http.Client {
	if http2Enabled {
		return http.Client{
			Transport: &http2.Transport{
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return (&net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}).DialContext(ctx, network, addr)
				},
			},
			Timeout: 90 * time.Second,
		}
	}
	return http.Client{
		Transport: &http.Transport{
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
		},
		Timeout: 90 * time.Second,
	}
}

func SendMessage(ctx context.Context, httpClient http.Client, wsgwUrl string, userId string, message *dto.E2EMessage, wsConnIds []string, discardConnId func(connId string)) error {
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
