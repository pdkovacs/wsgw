package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/test/mockapp"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"
)

var defaultConnectOptions = &websocket.DialOptions{
	HTTPHeader: http.Header{
		"Authorization": []string{"some credentials"},
	},
}

type Client struct {
	wsConn         *websocket.Conn
	connectionId   wsgw.ConnectionID
	proxyUrl       string
	msgFromAppChan chan string
}

func NewClient(proxyUrl string, msgFromAppChan chan string) *Client {
	return &Client{
		proxyUrl:       proxyUrl,
		msgFromAppChan: msgFromAppChan,
	}
}

func (c *Client) connect(ctx context.Context, connectOptions ...*websocket.DialOptions) (*http.Response, error) {
	conn, httpResponse, err := connectTowsgw(ctx, c.proxyUrl, connectOptions...)
	if err != nil {
		return httpResponse, err
	}
	c.wsConn = conn

	c.connectionId = wsgw.ConnectionID(httpResponse.Header.Get(wsgw.ConnectionIDHeaderKey))

	go func() {
		readFromAppLogger := zerolog.Ctx(ctx).With().Logger()
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
			c.msgFromAppChan <- string(msgFromApp)
		}
	}()

	return httpResponse, nil
}

func (c *Client) disconnect(_ context.Context) error {
	return c.wsConn.Close(websocket.StatusNormalClosure, "we're done")
}

func (c *Client) writeMessage(ctx context.Context, message mockapp.MessageJSON) error {
	return wsjson.Write(ctx, c.wsConn, message)
}

func connectTowsgw(ctx context.Context, proxyUrl string, connectOptions ...*websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	options := defaultConnectOptions
	if connectOptions != nil {
		options = connectOptions[0]
	}
	return websocket.Dial(ctx, fmt.Sprintf("ws://%s%s", proxyUrl, wsgw.ConnectPath), options)
}
