package httpadapter

import (
	"fmt"
	"net/http"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/config"
	"wsgw/test/e2e/app/security/authn"
	"wsgw/test/e2e/app/security/authr"
	"wsgw/test/e2e/app/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const UserKey = "iconrepo-user"

type SessionData struct {
	UserInfo authr.UserInfo
}

func mustGetUserSession_(c *gin.Context) SessionData {
	session := sessions.Default(c)
	user := session.Get(UserKey)
	if userSession, ok := user.(SessionData); ok {
		return userSession
	}
	panic(fmt.Errorf("unexpected user session type: %T", user))
}

func getUserInfo(authnType authn.AuthenticationScheme) func(c *gin.Context) (authr.UserInfo, error) {
	return func(c *gin.Context) (authr.UserInfo, error) {
		switch authnType {
		case authn.SchemeBasic:
			{
				sessionData := mustGetUserSession_(c)
				return sessionData.UserInfo, nil
			}
		case authn.SchemeOIDC:
			{
				sessionData := mustGetUserSession_(c)
				return sessionData.UserInfo, nil
			}
		case authn.SchemeOIDCProxy:
			{
				uInfoCandidate, exists := c.Get(forwardedUserInfoKey)
				if !exists {
					return authr.UserInfo{}, fmt.Errorf("user-info key missing from request context")
				}
				if userInfo, ok := uInfoCandidate.(authr.UserInfo); ok {
					return userInfo, nil
				}
				panic(fmt.Sprintf("failed to cast to UserInfo: %#v", uInfoCandidate))
			}
		}
		panic(fmt.Sprintf("unexpected authentication type: %v", authnType))
	}
}

func authenticationCheck(options config.Options, userService *services.UserService) gin.HandlerFunc {
	switch options.AuthenticationType {
	case authn.SchemeBasic:
		return checkBasicAuthentication(basicConfig{PasswordCredentialsList: options.PasswordCredentials}, *userService)
	case authn.SchemeOIDC:
		return checkOIDCAuthentication()
	case authn.SchemeOIDCProxy:
		return checkOIDCProxyAuthentication(userService.AuthorizationService)
	}
	panic(fmt.Sprintf("unexpected authentication type: %v", options.AuthenticationType))
}

// authentication handles authentication
func authentication(options config.Options, userService *services.UserService, log zerolog.Logger) gin.HandlerFunc {
	logger := log.With().Str(logging.FunctionLogger, "authentication").Logger()
	logger.Debug().Str("authenticationType", string(options.AuthenticationType)).Msg("Setting up authentication framework")
	switch options.AuthenticationType {
	case authn.SchemeBasic:
		return basicScheme(basicConfig{PasswordCredentialsList: options.PasswordCredentials}, userService)
	case authn.SchemeOIDC:
		return CreateOIDCSChemeHandler(oidcConfig{
			tokenIssuer:           options.OIDCTokenIssuer,
			clientRedirectBackURL: options.OIDCClientRedirectBackURL,
			clientID:              options.OIDCClientID,
			clientSecret:          options.OIDCClientSecret,
			serverURLContext:      options.ServerURLContext,
		}, userService, options.UsernameCookie, log)
	case authn.SchemeOIDCProxy:
		return func(g *gin.Context) {
		}
	}
	return nil
}

func logout(options config.Options) gin.HandlerFunc {
	return func(g *gin.Context) {
		if options.AuthenticationType != authn.SchemeOIDC {
			log.Info().Str("authenticationType", string(options.AuthenticationType)).Msg("Logout is not currently supported with authentication scheme")
			g.AbortWithStatus(http.StatusBadRequest)
		}
		session := sessions.Default(g)
		session.Clear() // this will mark the session as "written" only if there's
		// at least one key to delete
		session.Options(sessions.Options{MaxAge: -1})
		session.Save()
		g.Abort()
		g.Redirect(http.StatusFound, options.OIDCLogoutURL)
	}
}
