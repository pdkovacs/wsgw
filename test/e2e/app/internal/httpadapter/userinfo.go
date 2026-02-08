package httpadapter

import (
	"fmt"
	"net/http"
	"wsgw/test/e2e/app/pgks/security"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func userListHandler(userService security.UserService, passwordCreds []security.PasswordCredentials) func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Logger()

		users, err := userService.GetUsers(g, passwordCreds)
		if err != nil {
			logger.Error().Err(err).Msg("failed to to retreive user-list")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		g.JSON(200, users)
	}
}

func userInfoHandler(userService security.UserService) func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context())
		userId := g.Query("userId")

		session := sessions.Default(g)
		user := session.Get(security.UserKey)

		usession, ok := user.(security.SessionData)
		if !ok {
			logger.Error().Type("user", user).Msg("failed to cast user session")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		var userInfo security.UserInfo
		if userId == "" {
			userInfo = usession.UserInfo
		} else {
			userInfo = userService.GetUserInfo(g, userId)
		}
		if logger.GetLevel() == zerolog.DebugLevel {
			logger.Debug().Interface("user-info", userInfo).Msg("user info reetrieved")
		}

		g.JSON(200, userInfo)
	}
}

func getSessionDataFromSession(g *gin.Context, logger *zerolog.Logger) (security.SessionData, bool) {
	session := sessions.Default(g)
	userSessionData := session.Get(security.UserKey)
	if userSessionData == nil {
		logger.Error().Msg("no session found")
		g.AbortWithStatus(http.StatusUnauthorized)
		return security.SessionData{}, false
	}

	if userSessionData, ok := userSessionData.(security.SessionData); ok {
		return userSessionData, true
	}

	logger.Warn().Str("sessionDataType", fmt.Sprintf("%T", userSessionData)).Msg("failed to cast session data to SessionData")
	return security.SessionData{}, false
}
