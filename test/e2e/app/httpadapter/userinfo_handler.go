package httpadapter

import (
	"net/http"
	"wsgw/test/e2e/app/config"
	"wsgw/test/e2e/app/security/authn"
	"wsgw/test/e2e/app/security/authr"
	"wsgw/test/e2e/app/services"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

type userInfoDTO struct {
	Username    string               `json:"username"`
	Groups      []authr.GroupID      `json:"groups"`
	Permissions []authr.PermissionID `json:"permissions"`
	DisplayName string               `json:"displayName"`
}

func usersByGroupsToUserIDList(usersByRoles config.UsersByRoles) []string {
	result := []string{}

	userIDListProto := make(map[string]struct{})

	for _, users := range usersByRoles {
		for _, user := range users {
			userIDListProto[user] = struct{}{}
		}
	}

	for user := range userIDListProto {
		result = append(result, user)
	}

	return result
}

func userListHandler(userService services.UserService) func(c *gin.Context) {
	return func(g *gin.Context) {
		usersByGroups := userService.AuthorizationService.GetUsers()
		g.JSON(200, usersByGroupsToUserIDList(usersByGroups))
	}
}

func userInfoHandler(authType authn.AuthenticationScheme, userService services.UserService) func(c *gin.Context) {
	return func(g *gin.Context) {
		logger := zerolog.Ctx(g.Request.Context())
		userId := g.Query("userId")

		if authType == authn.SchemeOIDCProxy {
			userInfo, getUserInfoErr := getUserInfo(authType)(g)
			if getUserInfoErr != nil {
				logger.Error().Msg("failed to find user-info")
				g.AbortWithStatus(401)
				return
			}

			responseUserInfo := userInfoDTO{
				Username:    userInfo.UserId,
				Groups:      userInfo.Groups,
				Permissions: userInfo.Permissions,
				DisplayName: userInfo.DisplayName,
			}

			g.JSON(200, responseUserInfo)
			return
		}

		session := sessions.Default(g)
		user := session.Get(UserKey)

		usession, ok := user.(SessionData)
		if !ok {
			logger.Error().Type("user", user).Msg("failed to cast user session")
			g.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		var userInfo authr.UserInfo
		if userId == "" {
			userInfo = usession.UserInfo
		} else {
			userInfo = userService.GetUserInfo(g, userId)
		}
		if logger.GetLevel() == zerolog.DebugLevel {
			logger.Debug().Interface("user-info", userInfo).Msg("user info reetrieved")
		}

		responseUserInfo := userInfoDTO{
			Username:    userInfo.UserId,
			Groups:      userInfo.Groups,
			Permissions: userInfo.Permissions,
			DisplayName: userInfo.DisplayName,
		}

		g.JSON(200, responseUserInfo)
	}
}
