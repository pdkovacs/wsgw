package httpadapter

import (
	"io"
	"net/http"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/conntrack"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

var userWsConnmap = conntrack.NewUserWsConnectionMap()

func connectWsHandler() func(g *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "connectWsHandler").Logger()
		req := g.Request
		res := g

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			logger.Debug().Str(wsgw.ConnectionIDKey, connId).Str("connid", connId).Msg("incoming connection request...")

			userSessionData, ok := getSessionDataFromSession(g, &logger)
			if !ok {
				return
			}

			userWsConnmap.AddConnection(userSessionData.UserInfo.UserId, connId)

			res.Status(http.StatusOK)
			return
		}

		logger.Info().Msg("incoming ws connection request without connection-id")
		res.Status(http.StatusBadRequest)
	}
}

func disconnectWsHandler() func(g *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.MethodLogger, "connectWsHandler").Logger()
		req := g.Request

		connHeaderKey := wsgw.ConnectionIDHeaderKey
		if connId := req.Header.Get(connHeaderKey); connId != "" {
			logger.Debug().Str(wsgw.ConnectionIDKey, connId).Str("connid", connId).Msg("incoming disconnection request...")

			userSessionData, ok := getSessionDataFromSession(g, &logger)
			if !ok {
				return
			}

			userId := userSessionData.UserInfo.UserId

			if !userWsConnmap.RemoveConnection(userId, connId) {
				logger.Info().Str("userId", userId).Msg("user has no ws connections")
			}

			return
		}

		logger.Info().Msg("incoming ws disconnection request without connection-id")
		g.Status(http.StatusBadRequest)
	}
}

func messageWsHanlder() func(g *gin.Context) {
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
