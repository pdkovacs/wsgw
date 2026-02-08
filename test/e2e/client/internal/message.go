package internal

import (
	"encoding/json"
	"time"
	"wsgw/test/e2e/app/pgks/dto"
	"wsgw/test/e2e/app/pgks/security"

	"go.opentelemetry.io/otel/trace"
)

type recipientName string

type message struct {
	testRunId  string
	id         string
	text       string
	sender     security.PasswordCredentials
	recipients []recipientName
	sentAt     time.Time
	span       trace.Span
}

func parseMsg(msgStr string) (*dto.E2EMessage, error) {
	var msgDto dto.E2EMessage
	unmarshalError := json.Unmarshal([]byte(msgStr), &msgDto)
	return &msgDto, unmarshalError
}
