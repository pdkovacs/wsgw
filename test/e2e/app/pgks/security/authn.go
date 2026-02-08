package security

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

const UserKey = "iconrepo-user"

type SessionData struct {
	UserInfo UserInfo
}

func AuthenticationCheck(passwdCredentials []PasswordCredentials, userService *UserService) gin.HandlerFunc {
	return checkBasicAuthentication(basicConfig{PasswordCredentialsList: passwdCredentials}, *userService)
}

// Authentication handles Authentication
func Authentication(passwdCredentials []PasswordCredentials, userService *UserService, log zerolog.Logger) gin.HandlerFunc {
	logger := log.With().Logger()
	logger.Debug().Msg("Setting up basic authentication framework")
	return basicScheme(basicConfig{PasswordCredentialsList: passwdCredentials}, userService)
}
