package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/app/pgks/dto"

	"github.com/rs/zerolog"
)

var httpClient http.Client = http.Client{
	Timeout: time.Second * 15,
}

func SendMessage(ctx context.Context, wsgwUrl string, userId string, message *dto.E2EMessage, wsConnIds []string, discardConnId func(connId string)) error {
	logger0 := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "messaging").Str("wsgwUrl", wsgwUrl).Str(logging.FunctionLogger, "SendMessage").Logger()
	logger0.Debug().Str("recipient", userId).Any("message", message).Int("targetConnectionCount", len(wsConnIds)).Msg("message to send...")

	var err error

	messageAsString, marshalErr := json.Marshal(message)
	if marshalErr != nil {
		logger0.Error().Err(marshalErr).Any("messageIn", message).Msg("failed to marshal message")
	}

	for _, connId := range wsConnIds {
		url := fmt.Sprintf("%s%s/%s", wsgwUrl, wsgw.MessagePath, connId)
		logger := logger0.With().Str("url", url).Logger()

		logger.Debug().Msg("address to send to...")
		req, createReqErr := http.NewRequest(http.MethodPost, url, strings.NewReader(string(messageAsString)))
		if createReqErr != nil {
			logger.Error().Err(createReqErr).Msg("failed to create request")
			continue
		}
		response, sendReqErr := httpClient.Do(req)
		if sendReqErr != nil {
			logger.Error().Err(sendReqErr).Msg("failed to send request")
			err = errors.Join(err, sendReqErr)
			continue
		}

		if response.StatusCode != http.StatusNoContent {
			if response.StatusCode == http.StatusNotFound {
				discardConnId(connId)
				logger.Debug().Str("connId", connId).Msg("404: discarding ws connection reference")
				continue
			}

			logger.Error().Str("url", url).Msg("failed to send request")
			err = errors.Join(err, fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode))
			continue
		}
	}

	return err
}
