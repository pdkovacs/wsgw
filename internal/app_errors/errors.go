package app_errors

type BadRequest struct {
	message string
}

func (m *BadRequest) Error() string {
	return m.message
}

func NewBadRequest(message string) *BadRequest {
	return &BadRequest{message}
}
