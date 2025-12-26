package dto

type E2EMessage struct {
	TestRunId   string   `json:"runId"`
	Id          string   `json:"id"`
	Sender      string   `json:"sender"`
	Recipients  []string `json:"recipients"`
	Data        string   `json:"data"`
	SentAt      string   `json:"sentAt"`
	TracingData string   `json:"tracingData"`
}
