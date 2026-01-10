package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	math_rand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/app/pgks/dto"
	"wsgw/test/e2e/client/internal/config"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var httpClient http.Client = http.Client{
	Timeout: time.Second * 15,
}

type recipientName string

type Message struct {
	testRunId  string
	id         string
	text       string
	sender     config.PasswordCredentials
	recipients []recipientName
	sentAt     time.Time
	span       trace.Span
}

func (msg *Message) sendMessage(ctx context.Context, endpoint string) error {
	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "sendMessage").Str("endpoint", endpoint).Str("messageId", msg.id).Logger()

	logger.Debug().Msg("BEGIN")

	recips := []string{}
	for _, recip := range msg.recipients {
		recips = append(recips, string(recip))
	}

	msgDto := &dto.E2EMessage{
		TestRunId:  msg.testRunId,
		Id:         msg.id,
		Sender:     msg.sender.Username,
		Recipients: recips,
		Data:       msg.text,
		// the test-app will basically ignore this for now, it will start a new root context for each push
		TraceData: monitoring.InjectTraceData(ctx),
	}

	message, marshalErr := json.Marshal(msgDto)
	if marshalErr != nil {
		logger.Error().Err(marshalErr).Msg("failed to marshal message")
	}

	req, createReqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(message)))
	if createReqErr != nil {
		logger.Error().Err(createReqErr).Msg("failed to create request")
		return createReqErr
	}
	req.Header = createBasicAuthnHeader(msg.sender.Username, msg.sender.Password)

	tracer := otel.Tracer(config.OtelScope)
	ctx, msg.span = tracer.Start(
		ctx,
		"request-sending-message",
		trace.WithAttributes(attribute.String("runId", msgDto.TestRunId)),
	)
	monitoring.InjectIntoHeader(ctx, req.Header)

	response, sendReqErr := httpClient.Do(req)
	if sendReqErr != nil {
		logger.Error().Err(sendReqErr).Msgf("failed to send request: %#v", sendReqErr)
		return sendReqErr
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}()

	if response.StatusCode != http.StatusNoContent {
		logger.Error().Str("endpoint", endpoint).Int("statusCode", response.StatusCode).Msg("failed to send request")
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	logger.Debug().Msg("END")
	return nil
}

// TODO:
// For now, recipients will be usernames, but it really should be connectionIds (userDevices aka. destinations).
// The test-client could actually know of the connectionIds: see WSGW_ACK_NEW_CONN_WITH_CONN_ID (AckNewConnWithConnId)
type pendingDeliveryTracker struct {
	pendingDeliveries map[string]*Message
	lock              sync.RWMutex
	notifyEmpty       func()
}

func newMessagesById() *pendingDeliveryTracker {
	return &pendingDeliveryTracker{
		pendingDeliveries: map[string]*Message{},
		lock:              sync.RWMutex{},
	}
}

func (mr *pendingDeliveryTracker) addPending(msg *Message) {
	mr.lock.Lock()
	defer mr.lock.Unlock()
	mr.pendingDeliveries[msg.id] = msg
}

func (mr *pendingDeliveryTracker) get(msgId string) *Message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	msg, has := mr.pendingDeliveries[msgId]

	if !has {
		return nil
	}

	return msg
}

// Returns the message, if it was delivered to all recipients
func (mr *pendingDeliveryTracker) markDelivered(msgId string, recipient recipientName) *Message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	defer func() {
		if mr.notifyEmpty != nil && len(mr.pendingDeliveries) == 0 {
			mr.notifyEmpty()
		}
	}()

	msg, has := mr.pendingDeliveries[msgId]
	if !has {
		return nil
	}

	updatedRecipList := []recipientName{}
	for _, recip := range msg.recipients {
		if recip == recipient {
			continue
		}
		updatedRecipList = append(updatedRecipList, recip)
	}

	if len(updatedRecipList) == 0 {
		delete(mr.pendingDeliveries, msgId)
		return msg
	}

	msg.recipients = updatedRecipList
	mr.pendingDeliveries[msgId] = msg

	return nil
}

func (mr *pendingDeliveryTracker) watchDraining(notifyEmpty func()) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	if len(mr.pendingDeliveries) == 0 {
		notifyEmpty()
	}

	mr.notifyEmpty = notifyEmpty
}

func selectRecipients(candidates []string, howMany int) []string {
	n := len(candidates)
	if howMany >= n {
		return candidates
	}

	result := make([]string, 0, howMany)
	k := howMany // elements still needed

	for i := range n {
		remaining := n - i // elements left to visit (including current)

		// Probability of selecting current element = k / remaining
		if math_rand.Intn(remaining) < k {
			result = append(result, candidates[i])
			k-- // one less element needed
			if k == 0 {
				break // done selecting
			}
		}
	}

	return result
}

func parseMsg(msgStr string) (*dto.E2EMessage, error) {
	var msgDto dto.E2EMessage
	unmarshalError := json.Unmarshal([]byte(msgStr), &msgDto)
	return &msgDto, unmarshalError
}
