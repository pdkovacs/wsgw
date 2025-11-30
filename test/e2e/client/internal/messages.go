package internal

import (
	"context"
	"encoding/json"
	"fmt"
	math_rand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/app/pgks/dto"
	"wsgw/test/e2e/client/internal/config"

	"github.com/rs/zerolog"
)

type recipientId string

type Message struct {
	testRunId  string
	id         string
	text       string
	sender     config.PasswordCredentials
	recipients []recipientId
	sentAt     time.Time
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
		SentAt:     time.Now().String(),
	}

	message, marshalErr := json.Marshal(msgDto)
	if marshalErr != nil {
		logger.Error().Err(marshalErr).Msg("failed to marshal message")
	}

	req, createReqErr := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(message)))
	if createReqErr != nil {
		logger.Error().Err(createReqErr).Msg("failed to create request")
		return createReqErr
	}
	req.Header = createBasicAuthnHeader(msg.sender.Username, msg.sender.Password)
	client := http.Client{
		Timeout: time.Second * 15,
	}
	response, sendReqErr := client.Do(req)
	if sendReqErr != nil {
		logger.Error().Err(sendReqErr).Msgf("failed to send request: %#v", sendReqErr)
		return sendReqErr
	}

	if response.StatusCode != http.StatusNoContent {
		logger.Error().Str("endpoint", endpoint).Int("statusCode", response.StatusCode).Msg("failed to send request")
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	logger.Debug().Msg("END")
	return nil
}

type messageAndDeliveries struct {
	msg        *Message
	recipients []recipientId
}

type pendingDeliveriesByMsgId struct {
	deliveries  map[string]messageAndDeliveries
	lock        sync.RWMutex
	notifyEmpty func()
}

func newMessagesById() *pendingDeliveriesByMsgId {
	return &pendingDeliveriesByMsgId{
		deliveries: map[string]messageAndDeliveries{},
		lock:       sync.RWMutex{},
	}
}

func (mr *pendingDeliveriesByMsgId) add(msg *Message) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	msgDelivs := messageAndDeliveries{
		msg:        msg,
		recipients: []recipientId{},
	}
	for _, recip := range msg.recipients {
		msgDelivs.recipients = append(msgDelivs.recipients, recip)
	}
	mr.deliveries[msg.id] = msgDelivs
}

func (mr *pendingDeliveriesByMsgId) get(msgId string) *Message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	deliveries, has := mr.deliveries[msgId]

	if !has {
		return nil
	}

	return deliveries.msg
}

func (mr *pendingDeliveriesByMsgId) remove(msgId string, recipient recipientId) bool {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	defer func() {
		if mr.notifyEmpty != nil && len(mr.deliveries) == 0 {
			mr.notifyEmpty()
		}
	}()

	deliveries, has := mr.deliveries[msgId]
	if !has {
		return false
	}

	found := false
	updatedRecipList := []recipientId{}
	for _, recip := range deliveries.recipients {
		if recip == recipient {
			found = true
			continue
		}
		updatedRecipList = append(updatedRecipList, recip)
	}

	if len(updatedRecipList) == 0 {
		delete(mr.deliveries, msgId)
		return found
	}

	updatedDelivs := deliveries
	updatedDelivs.recipients = updatedRecipList
	mr.deliveries[msgId] = updatedDelivs

	return found
}

func (mr *pendingDeliveriesByMsgId) watchDraining(notifyEmpty func()) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	if len(mr.deliveries) == 0 {
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

	for i := 0; i < n; i++ {
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
