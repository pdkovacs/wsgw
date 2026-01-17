package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"sync"
	"wsgw/internal/app_errors"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/conntrack"
	"wsgw/test/e2e/app/internal/services"
	"wsgw/test/e2e/app/pgks/dto"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
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
		// TODO: Maybe use some concurrency budget
		// TODO: establish the request's own timeout from a query parameter
		requestCtx := context.WithoutCancel(g.Request.Context())
		h.metrics.messageRequestCounter.Add(requestCtx, 1)

		logger := zerolog.Ctx(requestCtx).With().Str(logging.MethodLogger, "api/messageHandler").Logger()
		context.AfterFunc(requestCtx, func() {
			logger.Debug().Err(context.Cause(requestCtx)).Str("stack", string(debug.Stack())).Msg("requestCtx canceled")
		})

		body, errReadBody := io.ReadAll(g.Request.Body)
		if errReadBody != nil {
			logger.Error().Err(errReadBody).Msg("failed to read request body")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}
		var messageIn dto.E2EMessage
		errMessageUnmarshal := json.Unmarshal(body, &messageIn)
		if errMessageUnmarshal != nil {
			logger.Error().Err(errMessageUnmarshal).Msg("failed to unmarshal request body into dto.E2EMessage")
			g.AbortWithStatus(http.StatusBadRequest)
			return
		}

		ctx := monitoring.ExtractFromHeader(requestCtx, g.Request.Header)

		tracer := otel.Tracer(config.OtelScope)
		tmpCtx, handleMsgReqSpan := tracer.Start(ctx, "handle-send-message-request")
		defer handleMsgReqSpan.End()
		ctx = tmpCtx

		logger = logger.With().Str("testRunId", messageIn.TestRunId).Str("messageId", messageIn.Id).Logger()

		//-- A super rudimentary aggregation of the potentially many requests that will be issued here:
		status := http.StatusNoContent
		var err error

		//////////////////////////////////////////////////////////////////////
		//-- Process by workers
		//--------------------------------------------------------------------
		userIdQueue := make(chan string, 1000)
		maxNumWorkers := 4
		numWorkers := min(len(messageIn.Recipients), maxNumWorkers)

		wgWorkUnits := sync.WaitGroup{}
		wgWorkUnits.Add(len(messageIn.Recipients))

		worker := func(id int) {
			workerLogger := logger.With().Str("worker", fmt.Sprintf("messageHandlerWorker-%d", id)).Logger()
			workerCtx := workerLogger.WithContext(ctx)
			context.AfterFunc(ctx, func() {
				workerLogger.Debug().Str("stack", string(debug.Stack())).Msg("worker context canceled")
			})
			keepOn := true

			for keepOn {
				select {
				case userId, ok := <-userIdQueue:
					if !ok {
						workerLogger.Debug().Msg("no more work")
						keepOn = false
						break
					}
					workerLogger.Debug().Msg("working")

					// We don't want this metainfo to be a potentially huge payload, since we don't think push-messages
					// should have huge payloads.
					messageIn.Recipients = []string{userId}

					// Skip error logging, callees did it with enough detail already
					sendStatus, sendErrors := h.sendMessageToUserDevices(workerCtx, &messageIn, userId)
					if sendStatus != http.StatusNoContent {
						status = sendStatus
					}
					if sendErrors != nil {
						err = errors.Join(err, sendErrors)
					}
					wgWorkUnits.Done()
				case <-workerCtx.Done():
					workerLogger.Debug().Msg("ctx done")
					keepOn = false
				}
			}
			workerLogger.Debug().Msg("worker exiting")
		}
		for i := range numWorkers {
			go worker(i)
		}

		logger.Debug().Int("numRecipients", len(messageIn.Recipients)).Int("numWorkers", numWorkers).Msg("feeding workers")
		for _, userId := range messageIn.Recipients {
			userIdQueue <- string(userId)
		}
		logger.Debug().Msg("waiting for all recipients to be processed...")
		wgWorkUnits.Wait()
		logger.Debug().Msg("all recipients have been processed")
		close(userIdQueue)
		//////////////////////////////////////////////////////////////////////

		if err == nil {
			g.Status(status)
			return
		}

		g.AbortWithStatusJSON(status, err.Error())
	}
}

func (h *APIHandler) sendMessageToUserDevices(ctx context.Context, message *dto.E2EMessage, userId string) (int, error) {
	logger := zerolog.Ctx(ctx).With().Str(logging.MethodLogger, "sendMessageToUserDevices").Str("userId", userId).Logger()

	status := http.StatusNoContent

	tracer := otel.Tracer(config.OtelScope)
	tmpCtx, findDevicesSpan := tracer.Start(ctx, "find-user-devices")
	ctx = tmpCtx

	wsConnIds, err := h.wsConnections.GetConnections(ctx, userId)
	if err != nil {
		logger.Error().Err(err).Msg("failed to get wsgw connection-ids for user")
		status = http.StatusInternalServerError

		findDevicesSpan.End()
		return status, errors.Join(nil, err)
	}

	if wsConnIds == nil {
		logger.Info().Msg("user has no wsgw connection open")
		status = http.StatusNotFound

		findDevicesSpan.End()
		return status, err
	}

	deleteConnId := func(connId string) {
		h.wsConnections.RemoveConnection(ctx, userId, connId)
		h.metrics.staleWsConnIdCounter.Add(ctx, 1)
	}

	findDevicesSpan.End()

	errProcessMessage := services.SendMessage(ctx, h.wsgwUrl, userId, message, wsConnIds, deleteConnId)

	if errProcessMessage != nil {
		var badRequest *app_errors.BadRequest
		if errors.As(errProcessMessage, &badRequest) {
			status = http.StatusBadRequest
			err = badRequest
			return status, err
		}
		status = http.StatusInternalServerError
		return status, errProcessMessage
	}

	return status, err
}
