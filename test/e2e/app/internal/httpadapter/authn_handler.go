package httpadapter

import (
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/security/authn"
	"wsgw/test/e2e/app/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

const UserKey = "iconrepo-user"

type SessionData struct {
	UserInfo authn.UserInfo
}

func authenticationCheck(conf config.Config, userService *services.UserService) gin.HandlerFunc {
	return checkBasicAuthentication(basicConfig{PasswordCredentialsList: conf.PasswordCredentials}, *userService)
}

// authentication handles authentication
func authentication(conf config.Config, userService *services.UserService, log zerolog.Logger) gin.HandlerFunc {
	logger := log.With().Str(logging.FunctionLogger, "authentication").Logger()
	logger.Debug().Msg("Setting up basic authentication framework")
	return basicScheme(basicConfig{PasswordCredentialsList: conf.PasswordCredentials}, userService)
}
