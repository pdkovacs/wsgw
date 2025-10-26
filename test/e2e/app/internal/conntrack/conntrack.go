package conntrack

import "context"

type WsConnections interface {
	AddConnection(ctx context.Context, userId string, connId string) error
	RemoveConnection(ctx context.Context, userId string, connId string) (bool, error)
	GetConnections(ctx context.Context, userId string) ([]string, error)
}

func NewWsgwConnectionTracker(dynamodbUrl string) (WsConnections, error) {
	if len(dynamodbUrl) == 0 {
		return newUserWsgwConntracker(), nil
	}
	return NewDynamodbConntracker(dynamodbUrl)
}
