package httpadapter

import (
	"io"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/app/internal/conntrack"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type WSHandler struct {
	wsConnections conntrack.WsConnections
	metrics       *wsHandlerMetrics
}

func newWSHandler(
	wsConnections conntrack.WsConnections,
) *WSHandler {
	return &WSHandler{
		wsConnections: wsConnections,
		metrics:       newWSHandlerMetrics(),
	}
}

func (ws *WSHandler) connectWsHandler(wsConnections conntrack.WsConnections) func(g *gin.Context) {
	return func(g *gin.Context) {
		ws.metrics.connectRequestCounter.Add(g.Request.Context(), 1)

		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "connectWsHandler").Logger()
		req := g.Request
		res := g

		userSessionData, ok := getSessionDataFromSession(g, &logger)
		if !ok {
			logger.Debug().Msg("failed to get userSessionData")
			res.Status(http.StatusUnauthorized)
			return
		}

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			logger.Debug().Str("connid", connId).Msg("incoming connection request...")

			addErr := wsConnections.AddConnection(g.Request.Context(), userSessionData.UserInfo.UserId, connId)
			if addErr != nil {
				res.Status(http.StatusInternalServerError)
				return
			}

			res.Status(http.StatusOK)
			return
		}

		logger.Info().Msg("incoming ws connection request without connection-id")
		res.Status(http.StatusBadRequest)
	}
}

func (ws *WSHandler) disconnectWsHandler(wsConnections conntrack.WsConnections) func(g *gin.Context) {
	return func(g *gin.Context) {
		ws.metrics.disconnectRequestCounter.Add(g.Request.Context(), 1)

		logger0 := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "connectWsHandler").Logger()
		req := g.Request

		userSessionData, ok := getSessionDataFromSession(g, &logger0)
		if !ok {
			return
		}
		userId := userSessionData.UserInfo.UserId

		logger0 = logger0.With().Str("userId", userId).Logger()

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			logger := logger0.With().Str("connid", connId).Logger()
			logger.Debug().Msg("incoming disconnection request...")

			if success, errRemoveConn := wsConnections.RemoveConnection(g.Request.Context(), userId, connId); !success || errRemoveConn != nil {
				if errRemoveConn != nil {
					logger.Error().Err(errRemoveConn).Msg("failed to remove connection")
					g.Status(http.StatusInternalServerError)
					return
				}
				logger.Info().Msg("user has no ws connections")
			}
			g.Status(http.StatusOK)
			return
		}

		logger0.Info().Msg("incoming ws disconnection request without connection-id")
		g.Status(http.StatusBadRequest)
	}
}

func messageWsHandler() func(g *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "WS message handler").Logger()
		req := g.Request
		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			bodyAsBytes, readBodyErr := io.ReadAll(req.Body)
			req.Body.Close()
			if readBodyErr != nil {
				logger.Error().Err(readBodyErr).Send()
				return
			}

			message := parseMessageJSON(bodyAsBytes)
			logger.Debug().Str(wsgw.ConnectionIDKey, connId).Any("message", message).Msg("message received")
		}
	}
}
