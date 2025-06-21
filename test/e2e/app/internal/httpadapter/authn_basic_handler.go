package httpadapter

import (
	"encoding/base64"
	"strings"
	"wsgw/internal/logging"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// basicConfig holds the configuration for the Basic authentication scheme
type basicConfig struct {
	PasswordCredentialsList []config.PasswordCredentials
}

func decodeBasicAuthnHeaderValue(headerValue string) (userid string, password string, decodeOK bool) {
	s := strings.SplitN(headerValue, " ", 2)
	if len(s) != 2 {
		return "", "", false
	}

	b, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil {
		return "", "", false
	}

	pair := strings.SplitN(string(b), ":", 2)
	if len(pair) != 2 {
		return "", "", false
	}

	return pair[0], pair[1], true
}

func checkBasicAuthentication(options basicConfig, userService services.UserService) func(c *gin.Context) {
	return func(c *gin.Context) {
		logger := zerolog.Ctx(c.Request.Context()).With().Str(logging.UnitLogger, "basic authn handler").Logger()
		authenticated := false

		session := sessions.Default(c)
		user := session.Get(UserKey)
		if user != nil {
			authenticated = true
			logger.Debug().Bool("isAuthenticated", authenticated).Send()
		} else {
			logger.Debug().Bool("isAuthenticated", authenticated).Send()
			authnHeaderValue, hasHeader := c.Request.Header["Authorization"]
			logger.Debug().Bool("hasHeader", hasHeader).Send()
			if hasHeader {
				username, password, decodeOK := decodeBasicAuthnHeaderValue(authnHeaderValue[0])
				logger.Debug().Bool("headerCouldBeDecoded", decodeOK).Send()
				if decodeOK {
					logger.Debug().Str("username", username).Send()
					logger.Debug().Int("passwordCredentialsListLength", len(options.PasswordCredentialsList)).Send()
					for _, pc := range options.PasswordCredentialsList {
						logger.Debug().Str("currentUserName", pc.Username).Send()
						if pc.Username == username && pc.Password == password {
							logger.Debug().Msg("password matches")
							userId := username
							userInfo := userService.GetUserInfo(c, userId)
							logger.Debug().Interface("userInfo", userInfo)
							session.Set(UserKey, SessionData{userInfo})
							session.Save()
							authenticated = true
							augmentedContextLogger := zerolog.Ctx(c.Request.Context()).With().Str("req_user", userId).Logger()
							c.Request = c.Request.WithContext(augmentedContextLogger.WithContext(c.Request.Context()))
							break
						}
					}
				}
			}
		}
		session.Save()

		logger.Debug().Bool("authenticationSuccess", authenticated).Send()

		if authenticated {
			c.Next()
		} else {
			c.Header("WWW-Authenticate", "Basic")
			c.AbortWithStatus(401)
		}
	}
}

func basicScheme(options basicConfig, userService *services.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		checkBasicAuthentication(options, *userService)(c)
	}
}
