package internal

import (
	"context"
	"crypto/rand"
	"fmt"
	math_rand "math/rand"
	"time"
	"wsgw/test/e2e/app/pgks/security"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type client struct {
	credentials        security.PasswordCredentials
	ws                 *wsClient // currently used only for receiving push messages
	outsandingMessages *deliveryTracker
	monitoring         *clientMonitoring
	runId              string
}

func newClient(
	credentials security.PasswordCredentials,
	wsgwUri string,
	outsandingMessages *deliveryTracker,
	monitoring *clientMonitoring,
	runId string,
) *client {
	ws := newWSClient(wsgwUri)
	return &client{
		credentials:        credentials,
		ws:                 ws,
		outsandingMessages: outsandingMessages,
		monitoring:         monitoring,
		runId:              runId,
	}
}

func (cli *client) connectAndListen(ctx context.Context) error {
	logger := zerolog.Ctx(ctx).With().Str("user", cli.credentials.Username).Logger()

	_, connectErr := cli.ws.connect(ctx, cli.runId, cli.credentials.Username, cli.credentials.Password)
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
			case <-ctx.Done():
				logger.Debug().Msg("context cancelled")
				return
			}
		}
	}()

	logger.Debug().Msg("END")
	return nil
}

func (cli *client) createMessage(ctx context.Context, recipientCandidates []string) *message {
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

	msg := &message{
		testRunId:  cli.runId,
		id:         msgId,
		sentAt:     time.Now(),
		sender:     cli.credentials,
		recipients: convertedRecipId,
		text:       fmt.Sprintf("[%s] %s", msgId, "a test message"),
	}

	cli.monitoring.incMsgCreatedCounter(ctx)

	return msg
}

func (cli *client) processMsgFromApp(ctx context.Context, msgFromAppStr string) {
	receivedAt := time.Now()

	logger := zerolog.Ctx(ctx).With().Logger()
	logger.Debug().Str("msgFromAppStr", msgFromAppStr).Msg("BEGIN")

	msgFromApp, parseErr := parseMsg(msgFromAppStr)
	if parseErr != nil {
		logger.Error().Err(parseErr).Str("msgFromAppStr", msgFromAppStr).Msg("failed to parse message")
		cli.monitoring.incMsgParseErrCounter(ctx)
		return
	}

	logger = logger.With().Str("msgId", msgFromApp.Id).Str("destination", msgFromApp.Destination).Str("sender", msgFromApp.Sender).Logger()

	msgInRepo := cli.outsandingMessages.get(msgFromApp.Id)
	if msgInRepo == nil {
		logger.Error().Msg("incoming message not found amongst outstanding")
		cli.monitoring.incOutstandingMsgNotFoundCounter(ctx)
		return
	}

	// AddEvent before markDelivered so the span is guaranteed to still be open:
	// span.End() is deferred by the goroutine that receives the last delivery (i.e.
	// markDelivered returns non-nil), and markDelivered can only return non-nil after
	// every recipient has called it — meaning every goroutine has already AddEvent'd.
	sentAt, timeParsingErr := msgFromApp.GetSentAt()
	if timeParsingErr != nil {
		logger.Error().Err(timeParsingErr).Msg("failed to parse sentAt time")
		msgInRepo.span.SetStatus(codes.Error, "failed to parse sentAt time")
		msgInRepo.span.AddEvent("delivery-error",
			trace.WithAttributes(
				attribute.String("destination", msgFromApp.Destination),
				attribute.String("reason", "failed to parse sentAt"),
			),
		)
		// Must still mark delivered so this recipient doesn't block testRunDone forever.
		if msgDone := cli.outsandingMessages.markDelivered(msgFromApp.Id, recipientName(cli.credentials.Username)); msgDone != nil {
			msgDone.span.End()
		}
		return
	}

	deliveryDuration := receivedAt.Sub(sentAt)
	logger.Debug().Dur("deliveryDuration", deliveryDuration).Msg("recording duration")

	eventAttrs := []attribute.KeyValue{
		attribute.String("destination", msgFromApp.Destination),
		attribute.Int64("deliveryDurationMs", deliveryDuration.Milliseconds()),
	}
	if msgInRepo.text != msgFromApp.Data {
		cli.monitoring.incdMsgTextMismatchCounter(ctx)
		eventAttrs = append(eventAttrs, attribute.Bool("textMismatch", true))
		msgInRepo.span.SetStatus(codes.Error, "message text mismatch")
	}
	if deliveryDuration.Milliseconds() > 400 {
		logger.Debug().Dur("deliveryDuration", deliveryDuration).Msg("extra slow")
		eventAttrs = append(eventAttrs, attribute.Bool("slow", true))
	}
	msgInRepo.span.AddEvent("delivered", trace.WithAttributes(eventAttrs...))

	msgDone := cli.outsandingMessages.markDelivered(msgFromApp.Id, recipientName(cli.credentials.Username))
	if msgDone != nil {
		msgDone.span.End()
	}

	cli.monitoring.recordDeliveryDurationMs(ctx, deliveryDuration)
	cli.monitoring.recordDeliveryDurationUs(ctx, deliveryDuration)
}
