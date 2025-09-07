package services

import (
	"context"
	"wsgw/internal/app_errors"
	"wsgw/internal/logging"

	"github.com/rs/zerolog"
)

type Message struct {
	Whom string `json:"whom"`
	What string `json:"what"`
}

func ProcessMessage(ctx context.Context, message Message, userService UserService) error {
	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "messaging").Str(logging.FunctionLogger, "ProcessMessage").Logger()
	logger.Debug().Str("whom", message.Whom).Str("what", message.What).Msg("message received")
	userInfo := userService.GetUserInfo(ctx, message.Whom)
	if len(userInfo.Groups) == 0 {
		logger.Debug().Str("adressee", message.Whom).Msg("non-existent user")
		return app_errors.NewBadRequest("message to non-existant user")
	}

	logger.Debug().Str("adressee", message.Whom).Msg("sending message...")

	return nil
}
