package dto

import "time"

const timeLayout = "2006-01-02 15:04:05.000000000 -0700"

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
	m.SentAt = time.Now().Format(timeLayout)
}

func (m *E2EMessage) GetSentAt() (time.Time, error) {
	return time.Parse(timeLayout, m.SentAt)
}
