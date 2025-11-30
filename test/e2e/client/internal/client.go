package internal

import (
	"context"
	"crypto/rand"
	"fmt"
	math_rand "math/rand"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/client/internal/config"

	"github.com/rs/zerolog"
)

type client struct {
	credentials        config.PasswordCredentials
	ws                 *wsClient // currently used only for receiving push messages
	sendMessageApiUrl  string
	outsandingMessages *pendingDeliveriesByMsgId
	monitoring         *clientMonitoring
}

func newClient(
	credentials config.PasswordCredentials,
	wsgwUri string,
	sendMessageApiUrl string,
	outsandingMessages *pendingDeliveriesByMsgId,
	monitoring *clientMonitoring,
) *client {
	ws := newWSClient(wsgwUri)
	return &client{
		credentials:        credentials,
		ws:                 ws,
		outsandingMessages: outsandingMessages,
		sendMessageApiUrl:  sendMessageApiUrl,
		monitoring:         monitoring,
	}
}

func (cli *client) connectAndListen(ctx context.Context) error {
	logger := zerolog.Ctx(ctx).With().Str("user", cli.credentials.Username).Str(logging.UnitLogger, "connectAndListen").Logger()

	_, connectErr := cli.ws.connect(ctx, cli.credentials.Username, cli.credentials.Password)
	if connectErr != nil {
		logger.Error().Err(connectErr).Msg("failed to connect to test app")
		return connectErr
	}

	go func() {
		logger.Debug().Msg("waiting for messages")
		for {
			select {
			case msgFromApp := <-cli.ws.msgFromAppChan:
				logger.Debug().Str("msgFromApp", msgFromApp).Msg("received from msgFromAppChan")
				cli.processMsgFromApp(ctx, msgFromApp)
			case <-time.After(50 * time.Duration(time.Second)):
				logger.Debug().Msg("timeout")
				return
			}
		}
	}()

	logger.Debug().Msg("END")
	return nil
}

func (cli *client) run(ctx context.Context, runId string, allUserNames []string) {
	msg := cli.createMessage(ctx, runId, allUserNames)
	msg.sendMessage(ctx, cli.sendMessageApiUrl)
}

func (cli *client) createMessage(ctx context.Context, runId string, recipientCandidates []string) *Message {
	recipientCount := math_rand.Intn(len(recipientCandidates)/2-1) + len(recipientCandidates)/2
	cli.monitoring.incMsgRecipientToReachCounter(ctx, int64(recipientCount))
	recipients := selectRecipients(recipientCandidates, recipientCount)

	msgId := rand.Text()

	convertedRecipId := []recipientId{}
	for _, recip := range recipients {
		convertedRecipId = append(convertedRecipId, recipientId(recip))
	}

	msg := &Message{
		testRunId:  runId,
		id:         msgId,
		sentAt:     time.Now(),
		sender:     cli.credentials,
		recipients: convertedRecipId,
		text:       fmt.Sprintf("[%s] %s", msgId, "a test message"),
	}

	cli.outsandingMessages.add(msg)
	cli.monitoring.incMsgCreatedCounter(ctx)

	return msg
}

func (cli *client) processMsgFromApp(ctx context.Context, msgFromAppStr string) {
	receivedAt := time.Now()

	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "processMsgFromApp").Logger()
	logger.Debug().Str("msgFromAppStr", msgFromAppStr).Msg("BEGIN")

	msgFromApp, parseErr := parseMsg(msgFromAppStr)
	if parseErr != nil {
		logger.Error().Err(parseErr).Str("msgFromAppStr", msgFromAppStr).Msg("failed to parse message")
		cli.monitoring.incMsgParseErrCounter(ctx)
		return
	}

	logger = logger.With().Str("sender", msgFromApp.Sender).Logger()
	msgInRepo := cli.outsandingMessages.get(msgFromApp.Id)
	if msgInRepo == nil {
		logger.Debug().Str("query", msgFromAppStr).Msg("incoming message not outstanding")
		cli.monitoring.incOutstandingMsgNotFoundCounter(ctx)
		return
	}

	cli.outsandingMessages.remove(msgFromApp.Id, recipientId(cli.credentials.Username))

	if msgInRepo.text != msgFromApp.Data {
		cli.monitoring.incdMsgTextMismatchCounter(ctx)
	}

	deliveryDuration := receivedAt.Sub(msgInRepo.sentAt)
	logger.Debug().Dur("delivery", deliveryDuration).Msg("recording duration")
	cli.monitoring.recordDeliveryDurationMs(ctx, deliveryDuration)
	cli.monitoring.recordDeliveryDurationUs(ctx, deliveryDuration)
}
