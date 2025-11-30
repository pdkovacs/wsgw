package internal

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

type wsClient struct {
	wsgwUri        string
	msgFromAppChan chan string
}

func newWSClient(wsgwUri string) *wsClient {
	return &wsClient{
		wsgwUri:        wsgwUri,
		msgFromAppChan: make(chan string, 1000),
	}
}

func (cli *wsClient) connect(ctx context.Context, username string, password string) (*http.Response, error) {
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "wsClient.connect").Logger()
	dialOptions := createDialOptions(username, password)
	conn, httpResponse, err := wsConnect(ctx, cli.wsgwUri, dialOptions)
	if err != nil || httpResponse.StatusCode != http.StatusSwitchingProtocols {
		logger.Error().Err(err).Int("httpStatus", httpResponse.StatusCode).Str("wsgwUri", cli.wsgwUri).Msg("failed to connect to wsgw")
		return httpResponse, err
	}

	go func() {
		readFromAppLogger := logger.With().Str(logging.FunctionLogger, "readFromApp").Logger()
		for {
			readFromAppLogger.Debug().Msg("waiting for input on wsconn...")
			msgType, msgFromApp, readErr := conn.Read(ctx)
			if readErr != nil {
				var closeError websocket.CloseError
				if errors.As(readErr, &closeError) && closeError.Code == websocket.StatusNormalClosure {
					readFromAppLogger.Debug().Msg("Client closed the connection normally")
					return
				}
				readFromAppLogger.Error().Err(readErr).Msg("error while reading from websocket")
				return
			}
			if msgType != websocket.MessageText {
				readFromAppLogger.Error().Int("message-type", int(msgType)).Msg("unexpected message-type read from websocket")
				return
			}
			readFromAppLogger.Debug().Str("msgFromApp", string(msgFromApp)).Msg("writing to msgFromAppChan...")
			cli.msgFromAppChan <- string(msgFromApp)
			readFromAppLogger.Debug().Str("msgFromApp", string(msgFromApp)).Msg("msgFromAppChan written to")
		}
	}()

	return httpResponse, nil
}

func createDialOptions(username string, password string) *websocket.DialOptions {

	return &websocket.DialOptions{
		HTTPHeader: createBasicAuthnHeader(username, password),
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
		return nil, nil, fmt.Errorf("failed to dial %s: %w", endpoint, err)
	}
	return conn, response, err
}
