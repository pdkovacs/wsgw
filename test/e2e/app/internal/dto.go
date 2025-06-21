package app

type SendNotificationRequest struct {
	UserID  string `json:"userId"`
	Payload string `json:"payload"`
}
