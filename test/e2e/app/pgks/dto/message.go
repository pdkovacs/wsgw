package dto

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
