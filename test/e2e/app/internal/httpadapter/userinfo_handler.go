package httpadapter

import (
	"fmt"
	"net/http"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/security/authn"
	"wsgw/test/e2e/app/internal/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

func userListHandler(userService services.UserService, passwordCreds []config.PasswordCredentials) func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context()).With().Str(logging.FunctionLogger, "userListHandler").Logger()

		users, err := userService.GetUsers(g, passwordCreds)
		if err != nil {
			logger.Error().Err(err).Msg("failed to to retreive user-list")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		g.JSON(200, users)
	}
}

func userInfoHandler(userService services.UserService) func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context())
		userId := g.Query("userId")

		session := sessions.Default(g)
		user := session.Get(UserKey)

		usession, ok := user.(SessionData)
		if !ok {
			logger.Error().Type("user", user).Msg("failed to cast user session")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		var userInfo authn.UserInfo
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

func getSessionDataFromSession(g *gin.Context, logger *zerolog.Logger) (SessionData, bool) {
	session := sessions.Default(g)
	userSessionData := session.Get(UserKey)
	if userSessionData == nil {
		logger.Error().Msg("no session found")
		g.AbortWithStatus(http.StatusUnauthorized)
		return SessionData{}, false
	}

	if userSessionData, ok := userSessionData.(SessionData); ok {
		return userSessionData, true
	}

	logger.Warn().Str("sessionDataType", fmt.Sprintf("%T", userSessionData)).Msg("failed to cast session data to SessionData")
	return SessionData{}, false
}
