package security

import (
	"context"
	"fmt"
)

func NewUserService() UserService {
	return UserService{}
}

type UserService struct {
}

func (us *UserService) getDisplayName(userId string) string {
	return userId
}

func (us *UserService) GetUserInfo(ctx context.Context, userId string) UserInfo {
	return UserInfo{
		UserId:      userId,
		DisplayName: us.getDisplayName(userId),
	}
}

func (us *UserService) GetUsers(ctx context.Context, passwordCreds []PasswordCredentials) ([]UserInfo, error) {
	if len(passwordCreds) > 0 {
		userInfoList := []UserInfo{}
		for _, creds := range passwordCreds {
			userInfoList = append(userInfoList, UserInfo{
				UserId:      creds.Username,
				DisplayName: creds.Username,
			})
		}
		return userInfoList, nil
	}
	return nil, fmt.Errorf("no password credentials")
}
