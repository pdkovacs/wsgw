package services

import (
	"context"
	"fmt"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/security/authn"
)

func NewUserService() UserService {
	return UserService{}
}

type UserService struct {
}

func (us *UserService) getDisplayName(userId string) string {
	return userId
}

func (us *UserService) GetUserInfo(ctx context.Context, userId string) authn.UserInfo {
	return authn.UserInfo{
		UserId:      userId,
		DisplayName: us.getDisplayName(userId),
	}
}

func (us *UserService) GetUsers(ctx context.Context, passwordCreds []config.PasswordCredentials) ([]authn.UserInfo, error) {
	if len(passwordCreds) > 0 {
		userInfoList := []authn.UserInfo{}
		for _, creds := range passwordCreds {
			userInfoList = append(userInfoList, authn.UserInfo{
				UserId:      creds.Username,
				DisplayName: creds.Username,
			})
		}
		return userInfoList, nil
	}
	return nil, fmt.Errorf("no password credentials")
}
