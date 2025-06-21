package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"

	"github.com/rs/zerolog"
)

type Message struct {
	Whom []string `json:"whom"`
	What string   `json:"what"`
}

func SendMessage(ctx context.Context, wsgwUrl string, userId string, message string, wsConnIds []string, discardConnId func(connId string)) error {
	logger0 := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "messaging").Str("wsgwUrl", wsgwUrl).Str(logging.FunctionLogger, "ProcessMessage").Logger()
	logger0.Debug().Str("adressee", userId).Str("what", message).Int("targetConnectionCount", len(wsConnIds)).Msg("message to send...")

	var err error

	wg := sync.WaitGroup{}
	for _, connId := range wsConnIds {
		wg.Go(
			func() {
				url := fmt.Sprintf("%s%s/%s", wsgwUrl, wsgw.MessagePath, connId)
				logger := logger0.With().Str("url", url).Logger()

				logger.Debug().Msg("address to send to...")
				req, createReqErr := http.NewRequest(http.MethodPost, url, strings.NewReader(message))
				if createReqErr != nil {
					logger.Error().Err(createReqErr).Msg("failed to create request")
					return
				}
				client := http.Client{
					Timeout: time.Second * 15,
				}
				response, sendReqErr := client.Do(req)
				if sendReqErr != nil {
					logger.Error().Err(sendReqErr).Msg("failed to send request")
					err = sendReqErr
					return
				}

				if response.StatusCode != http.StatusNoContent {
					if response.StatusCode == http.StatusNotFound {
						discardConnId(connId)
						logger.Info().Str("connId", connId).Msg("404: discarding ws connection reference")
						return
					}

					logger.Error().Str("url", url).Msg("failed to send request")
					err = fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
					return
				}
			},
		)
	}

	return err
}
