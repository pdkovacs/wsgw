package main

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/json"
	"fmt"
	math_rand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
	"wsgw/internal/logging"

	"github.com/rs/zerolog"
)

type messageDto struct {
	Id   string `json:"id"`
	Text string `json:"text"`
}

type Message struct {
	id         string
	text       string
	sender     string
	recipients []string
	sentAt     time.Time
}

type messagesById struct {
	messages map[string]*Message
	lock     sync.RWMutex
}

func newMessagesByClients() *messagesById {
	return &messagesById{
		messages: map[string]*Message{},
		lock:     sync.RWMutex{},
	}
}

func (mr *messagesById) add(msg *Message) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	mr.messages[msg.id] = msg
}

func (mr *messagesById) get(msgId string) *Message {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	return mr.messages[msgId]
}

func (mr *messagesById) remove(msgId string) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	delete(mr.messages, msgId)
}

var outsandingMessages *messagesById = newMessagesByClients()

func createMessage(sender string, recipientCandidates []string) *Message {
	recipientCount := math_rand.Intn(len(recipientCandidates))
	recipients := selectRecipients(recipientCandidates, recipientCount)

	msgId := crypto_rand.Text()

	msg := &Message{
		id:         msgId,
		sentAt:     time.Now(),
		sender:     sender,
		recipients: recipients,
		text:       msgId,
	}

	outsandingMessages.add(msg)

	return msg
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

func parseMsg(msgStr string) (*messageDto, error) {
	var msgDto messageDto
	unmarshalError := json.Unmarshal([]byte(msgStr), &msgDto)
	if unmarshalError != nil {
		panic(fmt.Sprintf("failed to parse '%s': %#v\n", msgStr, unmarshalError))
	}

	return &msgDto, unmarshalError
}

func SendMessage(ctx context.Context, endpoint string, msg *Message) error {
	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "sendMessage").Str("messageId", msg.id).Logger()

	msgDto := &messageDto{
		Id:   msg.id,
		Text: msg.text,
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
	client := http.Client{
		Timeout: time.Second * 15,
	}
	response, sendReqErr := client.Do(req)
	if sendReqErr != nil {
		logger.Error().Err(sendReqErr).Msg("failed to send request")
		return sendReqErr
	}

	if response.StatusCode != http.StatusNoContent {
		logger.Error().Str("endpoint", endpoint).Int("statusCode", response.StatusCode).Msg("failed to send request")
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	return nil
}
