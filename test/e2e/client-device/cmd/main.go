package main

import (
	"wsgw/internal/logging"
	app "wsgw/test/e2e/app"

	"github.com/rs/zerolog"
)

func main() {
	logger := logging.Get().Level(zerolog.GlobalLevel()).With().Str(logging.UnitLogger, "main").Logger()
	logger.Info().Msg("Test automation client device starting...")
}

func CreateNotificationRequest(userId string, payload string) *app.SendNotificationRequest {
	return nil
}
