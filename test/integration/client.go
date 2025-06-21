package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/test/mockapp"

	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
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

func (c *Client) readAck(ctx context.Context) (map[string]string, error) {
	ackMessageType, ackMessageBytes, errReadAckMessage := c.wsConn.Read(ctx)
	if errReadAckMessage != nil {
		return nil, errReadAckMessage
	}

	if websocket.MessageText != ackMessageType {
		return nil, fmt.Errorf("the type of ack-message is invalid: %v", ackMessageType)
	}

	var ackMessage map[string]string = map[string]string{}
	unmarshalErr := json.Unmarshal(ackMessageBytes, &ackMessage)
	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return ackMessage, nil
}

func (c *Client) readConnId(ctx context.Context) (wsgw.ConnectionID, error) {
	ackMessage, readAckErr := c.readAck(ctx)
	if readAckErr != nil {
		return "", readAckErr
	}

	return wsgw.ConnectionID(ackMessage[wsgw.ConnectionIDKey]), nil
}

func (c *Client) connect(ctx context.Context, connectOptions ...*websocket.DialOptions) (*http.Response, error) {
	conn, httpResponse, err := connectTowsgw(ctx, c.proxyUrl, connectOptions...)
	if err != nil {
		return httpResponse, err
	}
	c.wsConn = conn

	connId, readConnIdErr := c.readConnId(ctx)
	if readConnIdErr != nil {
		return httpResponse, readConnIdErr
	}
	c.connectionId = connId

	go func() {
		readFromAppLogger := zerolog.Ctx(ctx).With().Str("method", "readFromApp").Logger()
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
