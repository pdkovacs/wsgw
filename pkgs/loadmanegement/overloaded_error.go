package loadmanagement

import "time"

type OverloadError struct {
	RetryAfter time.Duration
	Reason     string
}

func (oe OverloadError) String() string {
	return oe.Reason
}

func (oe OverloadError) Error() string {
	return oe.Reason
}
