package internal

import (
	"context"
	"crypto/rand"
	"fmt"
	math_rand "math/rand"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/client/internal/config"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type client struct {
	credentials        config.PasswordCredentials
	ws                 *wsClient // currently used only for receiving push messages
	sendMessageApiUrl  string
	outsandingMessages *pendingDeliveryTracker
	monitoring         *clientMonitoring
}

func newClient(
	credentials config.PasswordCredentials,
	wsgwUri string,
	sendMessageApiUrl string,
	outsandingMessages *pendingDeliveryTracker,
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

func (cli *client) startTesting(ctx context.Context, runId string, allUserNames []string) {
	msg := cli.createMessage(ctx, runId, allUserNames)
	msg.sendMessage(ctx, cli.sendMessageApiUrl)
}

func (cli *client) createMessage(ctx context.Context, runId string, recipientCandidates []string) *Message {
	recipientCount := len(recipientCandidates)
	if recipientCount > 10 {
		recipientCount = math_rand.Intn(len(recipientCandidates)/2-1) + len(recipientCandidates)/2
	}
	cli.monitoring.incMsgRecipientToReachCounter(ctx, int64(recipientCount))
	recipients := selectRecipients(recipientCandidates, recipientCount)

	msgId := rand.Text()

	convertedRecipId := []recipientName{}
	for _, recip := range recipients {
		convertedRecipId = append(convertedRecipId, recipientName(recip))
	}

	msg := &Message{
		testRunId:  runId,
		id:         msgId,
		sentAt:     time.Now(),
		sender:     cli.credentials,
		recipients: convertedRecipId,
		text:       fmt.Sprintf("[%s] %s", msgId, "a test message"),
	}

	cli.outsandingMessages.addPending(msg)
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

	logger = logger.With().Str("msgId", msgFromApp.Id).Str("destination", msgFromApp.Destination).Logger()

	ctx = monitoring.ExtractTraceData(ctx, msgFromApp.TraceData)
	tracer := otel.Tracer(config.OtelScope)
	deliveryCtx, deliverySpan := tracer.Start(
		ctx,
		"wsgw-e2e-test-client-received-message",
		trace.WithAttributes(
			attribute.String("runId", msgFromApp.TestRunId),
			attribute.KeyValue{Key: "msgId", Value: attribute.StringValue(msgFromApp.Id)},
			attribute.KeyValue{Key: "destination", Value: attribute.StringValue(msgFromApp.Destination)},
		),
	)
	var msgDone *Message
	defer func() {
		if msgDone != nil {
			msgDone.span.End()
		}
		deliverySpan.End()
	}()

	logger = logger.With().Str("sender", msgFromApp.Sender).Logger()
	msgInRepo := cli.outsandingMessages.get(msgFromApp.Id)
	if msgInRepo == nil {
		cli.monitoring.incOutstandingMsgNotFoundCounter(deliveryCtx)
		deliverySpan.SetStatus(codes.Error, "incoming message not found amongst outstanding")
		return
	}

	msgDone = cli.outsandingMessages.markDelivered(msgFromApp.Id, recipientName(cli.credentials.Username))

	if msgInRepo.text != msgFromApp.Data {
		cli.monitoring.incdMsgTextMismatchCounter(deliveryCtx)
	}

	sentAt, timeParsingErr := msgFromApp.GetSentAt()
	if timeParsingErr != nil {
		logger.Error().Err(timeParsingErr).Msg("failed to parse sentAt time")
		deliverySpan.SetStatus(codes.Error, "failed to parse sentAt time")
		return
	}

	deliveryDuration := receivedAt.Sub(sentAt)
	logger.Debug().Dur("deliveryDuration", deliveryDuration).Msg("recording duration")
	if deliveryDuration.Milliseconds() > 400 {
		logger.Debug().Dur("deliveryDuration", deliveryDuration).Msg("extra slow")
	}
	cli.monitoring.recordDeliveryDurationMs(deliveryCtx, deliveryDuration)
	cli.monitoring.recordDeliveryDurationUs(deliveryCtx, deliveryDuration)
	deliverySpan.SetStatus(codes.Ok, "incoming message successfully processed")
}
