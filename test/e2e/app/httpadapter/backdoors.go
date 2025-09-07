package httpadapter

import (
	"encoding/json"
	"io"
	"net/http"

	"wsgw/test/e2e/app/security/authr"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func HandlePutIntoBackdoorRequest() func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context())

		requestBody, errReadRequest := io.ReadAll(g.Request.Body)
		if errReadRequest != nil {
			logger.Error().Type("request-body-type", g.Request.Body).Err(errReadRequest).Msg("failed to read request body")
			g.JSON(500, nil)
			return
		}
		permissions := []authr.PermissionID{}
		errBodyUnmarshal := json.Unmarshal(requestBody, &permissions)
		if errBodyUnmarshal != nil {
			logger.Error().Type("request-body-type", g.Request.Body).Err(errBodyUnmarshal).Msg("failed to unmarshal request body")
			g.JSON(400, nil)
			return
		}
		session := sessions.Default(g)
		user := session.Get(UserKey)
		logger.Info().Interface("user", user).Interface("permissions", permissions).Msg("authorization requested")

		sessionData, ok := user.(SessionData)
		if !ok {
			logger.Error().Type("session-data", user).Msg("failed to cast to SessionData")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		updatedCachedUserInfo := sessionData
		updatedCachedUserInfo.UserInfo.Permissions = permissions
		session.Set(UserKey, SessionData{updatedCachedUserInfo.UserInfo})
		session.Save()
		g.JSON(200, nil)
	}
}

func HandleGetIntoBackdoorRequest() func(g *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context())

		session := sessions.Default(g)
		user := session.Get(UserKey)
		sessionData, ok := user.(SessionData)
		if !ok {
			logger.Error().Type("session-data", user).Msg("failed to cast to SessionData")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		g.JSON(200, sessionData.UserInfo)
	}
}
