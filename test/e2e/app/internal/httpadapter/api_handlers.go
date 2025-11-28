package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type APIHandler struct {
	wsgwUrl       string
	wsConnections conntrack.WsConnections
	metrics       *apiHandlerMetrics
}

func newAPIHandler(
	wsgwUrl string,
	wsConnections conntrack.WsConnections,
) *APIHandler {
	return &APIHandler{
		wsgwUrl:       wsgwUrl,
		wsConnections: wsConnections,
		metrics:       newAPIHandlerMetrics(),
	}
}

func (h *APIHandler) messageHandler() func(g *gin.Context) {
	return func(g *gin.Context) {
		h.metrics.messageRequestCounter.Add(g.Request.Context(), 1)

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

		//-- A super rudimentary aggregation of the potentially many requests that will be issued here:
		status := http.StatusOK
		var err error

		//////////////////////////////////////////////////////////////////////
		//-- Process by workers
		//--------------------------------------------------------------------
		userIdQueue := make(chan string, 1000)
		numWorkers := 4

		worker := func(id int) {
			keepOn := true

			for keepOn {
				select {
				case userId, ok := <-userIdQueue:
					if !ok {
						logger.Debug().Str("worker", fmt.Sprintf("messageHandlerWorker-%d", id)).Msg("channel closed")
						keepOn = false
						break
					}
					logger.Debug().Str("worker", fmt.Sprintf("messageHandlerWorker-%d", id)).Msg("working")
					status, err = h.sendMessageToUserDevices(g.Request.Context(), messageIn.What, userId)
				case <-g.Request.Context().Done():
					logger.Info().Str("worker", fmt.Sprintf("messageHandlerWorker-%d", id)).Msg("ctx timeout")
					keepOn = false
				}
			}
			logger.Debug().Str("worker", fmt.Sprintf("messageHandlerWorker-%d", id)).Msg("finished")
		}

		wg := sync.WaitGroup{}
		for i := range numWorkers {
			wg.Go(func() { worker(i) })
		}

		for _, userId := range messageIn.Whom {
			userIdQueue <- userId
		}
		close(userIdQueue)
		wg.Wait()
		//////////////////////////////////////////////////////////////////////

		if err == nil {
			g.Status(status)
			return
		}

		g.AbortWithStatusJSON(status, err.Error())
	}
}

func (h *APIHandler) sendMessageToUserDevices(ctx context.Context, message string, userId string) (int, error) {
	var status int
	var err error

	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "sendMessageToUserDevices").Str("userId", userId).Logger()

	wsConnIds, getConnErr := h.wsConnections.GetConnections(ctx, userId)
	if getConnErr != nil {
		logger.Error().Err(getConnErr).Msg("failed to get wsgw connection-ids for user")
		status = http.StatusInternalServerError
		err = getConnErr
	}

	if wsConnIds == nil {
		logger.Info().Msg("user has no wsgw connection open")
		status = http.StatusNotFound
		return status, err
	}

	deleteConnId := func(connId string) {
		h.wsConnections.RemoveConnection(ctx, userId, connId)
		h.metrics.staleWsConnIdCounter.Add(ctx, 1)
	}
	errProcessMessage := services.SendMessage(ctx, h.wsgwUrl, userId, message, wsConnIds, deleteConnId)

	if errProcessMessage != nil {
		logger.Error().Err(errProcessMessage).Msg("failed to process message")
		var badRequest *app_errors.BadRequest
		if errors.As(errProcessMessage, &badRequest) {
			status = http.StatusBadRequest
			err = badRequest
			return status, err
		}
		status = http.StatusInternalServerError
		err = errProcessMessage
		return status, err
	}

	return status, err
}
