package dto

import "time"

type E2EMessage struct {
	TestRunId   string            `json:"runId"`
	Id          string            `json:"id"`
	Sender      string            `json:"sender"`
	Recipients  []string          `json:"recipients"`
	Data        string            `json:"data"`
	SentAt      string            `json:"sentAt"`
	Destination string            `json:"destination"` // aka. userDevice or connectionId
	TraceData   map[string]string `json:"traceData"`
}

func (m *E2EMessage) SetSentAt() {
	m.SentAt = time.Now().UTC().Format(time.RFC3339Nano)
}

func (m *E2EMessage) GetSentAt() (time.Time, error) {
	return time.Parse(time.RFC3339Nano, m.SentAt)
}
