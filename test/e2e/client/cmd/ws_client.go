package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

type wsClient struct {
	serverUri      string
	msgFromAppChan chan string
}

func newWSClient(serverUri string) *wsClient {
	return &wsClient{
		serverUri: serverUri,
	}
}

func (cli *wsClient) connect(ctx context.Context, username string, password string) (*http.Response, error) {
	dialOptions := createDialOptions(username, password)
	conn, httpResponse, err := wsConnect(ctx, cli.serverUri, dialOptions)
	if err != nil {
		return httpResponse, err
	}

	go func() {
		readFromAppLogger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "readFromApp").Logger()
		for {
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
			cli.msgFromAppChan <- string(msgFromApp)
		}
	}()

	return httpResponse, nil
}

func createDialOptions(username string, password string) *websocket.DialOptions {
	auth := username + ":" + password
	return &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte(auth))},
		},
	}
}

func wsConnect(ctx context.Context, serverUri string, connectOptions *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, fmt.Sprintf("ws://%s%s", serverUri, wsgw.ConnectPath), connectOptions)
}
