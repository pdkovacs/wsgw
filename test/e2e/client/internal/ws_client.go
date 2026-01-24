package internal

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/client/internal/config"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type wsClient struct {
	wsgwUri        string
	msgFromAppChan chan string
}

func newWSClient(wsgwUri string) *wsClient {
	return &wsClient{
		wsgwUri:        wsgwUri,
		msgFromAppChan: make(chan string, 1024*1024),
	}
}

func (cli *wsClient) connect(ctx context.Context, runId string, username string, password string) (*http.Response, error) {
	tracer := otel.Tracer(config.OtelScope)
	connectCtx, connectSpan := tracer.Start(
		ctx,
		"new-ws-connection",
		trace.WithAttributes(
			attribute.String("runId", runId),
		),
	)
	defer connectSpan.End()

	logger := zerolog.Ctx(connectCtx).With().Logger()
	dialOptions := createDialOptions(monitoring.InjectIntoHeader(connectCtx, http.Header{}), createBasicAuthnHeader(username, password))
	conn, httpResponse, err := wsConnect(connectCtx, cli.wsgwUri, dialOptions)
	if err != nil || httpResponse.StatusCode != http.StatusSwitchingProtocols {
		logger.Error().Err(err).Int("httpStatus", httpResponse.StatusCode).Str("wsgwUri", cli.wsgwUri).Msg("failed to connect to wsgw")
		return httpResponse, err
	}

	go func() {
		defer conn.Close(websocket.StatusNormalClosure, "closing connection")
		readFromAppLogger := logger.With().Logger()
		for {
			readFromAppLogger.Debug().Msg("waiting for input on wsconn...")
			msgType, msgFromApp, readErr := conn.Read(connectCtx)
			if readErr != nil {
				var closeError websocket.CloseError
				if errors.As(readErr, &closeError) && closeError.Code == websocket.StatusNormalClosure {
					readFromAppLogger.Debug().Msg("Client closed the connection normally")
					return
				}
				if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(readErr, context.Canceled) {
					logger.Debug().Msg("context done while reading from websocket")
					return
				}
				readFromAppLogger.Error().Err(readErr).Msg("error while reading from websocket")
				return
			}
			if msgType != websocket.MessageText {
				readFromAppLogger.Error().Int("message-type", int(msgType)).Msg("unexpected message-type read from websocket")
				continue
			}
			readFromAppLogger.Debug().Str("msgFromApp", string(msgFromApp)).Msg("writing to msgFromAppChan...")
			if cap(cli.msgFromAppChan) <= len(cli.msgFromAppChan)+1 {
				logger.Warn().Msg("msgFromAppChan at full capacity")
			}
			cli.msgFromAppChan <- string(msgFromApp)
			readFromAppLogger.Debug().Str("msgFromApp", string(msgFromApp)).Msg("msgFromAppChan written to")
		}
	}()

	return httpResponse, nil
}

func createDialOptions(headers ...http.Header) *websocket.DialOptions {
	header := http.Header{}

	for _, hrs := range headers {
		for hn, vals := range hrs {
			header.Add(hn, vals[0])
		}
	}

	return &websocket.DialOptions{
		HTTPHeader: header,
	}
}

func createBasicAuthnHeader(username string, password string) http.Header {
	auth := username + ":" + password
	return http.Header{
		"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(auth))},
	}
}

func wsConnect(ctx context.Context, wsgwUri string, connectOptions *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	endpoint := fmt.Sprintf("ws://%s%s", wsgwUri, wsgw.ConnectPath)
	conn, response, err := websocket.Dial(ctx, endpoint, connectOptions)
	if err != nil {
		return nil, response, fmt.Errorf("failed to dial %s: %w", endpoint, err)
	}
	return conn, response, err
}
