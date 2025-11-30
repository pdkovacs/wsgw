package main

import (
	"context"
	"fmt"
	"time"
)

type clientMonitoring struct {
	incMsgParseErrCounter            func(ctx context.Context)
	incOutstandingMsgNotFoundCounter func(ctx context.Context)
	incdMsgTextMismatchCounter       func(ctx context.Context)
	recordDeliveryDuration           func(ctx context.Context, deliveryTime time.Duration)
}

type Client struct {
	credentials passwordCredentials
	ws          *wsClient // currently used only for receiving push messages
	sendMessage func(msg *Message)
	monitoring  *clientMonitoring
}

func newClient(credentials passwordCredentials, conf config, sendMessage func(msg *Message), monitoring *clientMonitoring) *Client {
	ws := newWSClient(fmt.Sprintf("%s:%d", conf.ServerHostname, conf.ServerPort))
	return &Client{
		credentials: credentials,
		ws:          ws,
		sendMessage: sendMessage,
		monitoring:  monitoring,
	}
}

func (cli *Client) connect(ctx context.Context) error {
	_, connectErr := cli.ws.connect(ctx, cli.credentials.Username, cli.credentials.Password)
	if connectErr != nil {
		return connectErr
	}

	for {
		select {
		case msgFromApp := <-cli.ws.msgFromAppChan:
			cli.processMsgFromApp(ctx, msgFromApp)
		case <-ctx.Done():
			return nil
		}
	}
}

func (cli *Client) run(allUserNames []string) {
	msg := createMessage(cli.credentials.Username, allUserNames)
	cli.sendMessage(msg)
}

func (cli *Client) processMsgFromApp(ctx context.Context, msgFromAppStr string) {
	receivedAt := time.Now()

	msgFromApp, parseErr := parseMsg(msgFromAppStr)
	if parseErr != nil {
		cli.monitoring.incMsgParseErrCounter(ctx)
		return
	}

	msgInRepo := outsandingMessages.get(msgFromApp.Id)
	if msgInRepo == nil {
		cli.monitoring.incOutstandingMsgNotFoundCounter(ctx)
		return
	}

	outsandingMessages.remove(msgFromApp.Id)

	if msgInRepo.text != msgFromApp.Text {
		cli.monitoring.incdMsgTextMismatchCounter(ctx)
	}

	deliveryDuration := receivedAt.Sub(msgInRepo.sentAt)
	cli.monitoring.recordDeliveryDuration(ctx, deliveryDuration)
}
