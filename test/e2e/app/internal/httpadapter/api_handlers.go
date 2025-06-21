package httpadapter

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"wsgw/internal/app_errors"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func messageHandler(wsgwUrl string) func(g *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS connect handler").Logger()

		body, errReadBody := io.ReadAll(g.Request.Body)
		if errReadBody != nil {
			logger.Error().Err(errReadBody).Msg("failed to read request body")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}
		var messageIn services.Message
		errMessageUnmarshal := json.Unmarshal(body, &messageIn)
		if errMessageUnmarshal != nil {
			logger.Error().Err(errMessageUnmarshal).Msg("failed to unmarshal request body into services.Message")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}

		for _, userId := range messageIn.Whom {
			go func() {
				wsConnIds := userWsConnmap.GetConnections(userId)

				if wsConnIds == nil {
					logger.Info().Str("userId", userId).Msg("user has no ws connection open")
					g.Status(http.StatusNotFound)
					return
				}

				errProcessMessage := services.SendMessage(g.Request.Context(), wsgwUrl, userId, messageIn.What, wsConnIds, func(connId string) {
					userWsConnmap.RemoveConnection(userId, connId)
				})

				if errProcessMessage != nil {
					logger.Error().Err(errProcessMessage).Msg("failed to process message")
					var badRequest *app_errors.BadRequest
					if errors.As(errProcessMessage, &badRequest) {
						g.AbortWithStatusJSON(http.StatusBadRequest, badRequest.Error())
						return
					}
					g.AbortWithStatus(http.StatusInternalServerError)
					return
				}
			}()
		}

		g.Status(http.StatusOK)
	}
}
