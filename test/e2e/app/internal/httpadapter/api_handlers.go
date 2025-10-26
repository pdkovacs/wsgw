package httpadapter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"wsgw/internal/app_errors"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/conntrack"
	"wsgw/test/e2e/app/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func messageHandler(wsgwUrl string, wsConnections conntrack.WsConnections) func(g *gin.Context) {
	return func(g *gin.Context) {
		logger0 := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS connect handler").Logger()

		body, errReadBody := io.ReadAll(g.Request.Body)
		if errReadBody != nil {
			logger0.Error().Err(errReadBody).Msg("failed to read request body")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}
		var messageIn services.Message
		errMessageUnmarshal := json.Unmarshal(body, &messageIn)
		if errMessageUnmarshal != nil {
			logger0.Error().Err(errMessageUnmarshal).Msg("failed to unmarshal request body into services.Message")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}

		status := http.StatusOK
		var err error

		var wg sync.WaitGroup

		for _, userId := range messageIn.Whom {
			wg.Go(
				func() {
					logger := logger0.With().Str("userId", userId).Logger()

					wsConnIds, getConnErr := wsConnections.GetConnections(g.Request.Context(), userId)
					if getConnErr != nil {
						logger.Error().Err(getConnErr).Msg("failed to get wsgw connection-ids for user")
						status = http.StatusInternalServerError
						err = getConnErr
					}

					if wsConnIds == nil {
						logger.Info().Msg("user has no wsgw connection open")
						status = http.StatusNotFound
						return
					}

					errProcessMessage := services.SendMessage(g.Request.Context(), wsgwUrl, userId, messageIn.What, wsConnIds, func(connId string) {
						wsConnections.RemoveConnection(g.Request.Context(), userId, connId)
					})

					if errProcessMessage != nil {
						logger.Error().Err(errProcessMessage).Msg("failed to process message")
						var badRequest *app_errors.BadRequest
						if errors.As(errProcessMessage, &badRequest) {
							status = http.StatusBadRequest
							err = badRequest
							return
						}
						status = http.StatusInternalServerError
						err = errProcessMessage
						return
					}
				},
			)
		}

		wg.Wait()

		if err == nil {
			g.Status(status)
			return
		}

		g.AbortWithStatusJSON(status, err.Error())
	}
}
