package conntrack

import (
	"context"
	"fmt"
	"wsgw/test/e2e/app/internal/config"
)

type WsConnections interface {
	AddConnection(ctx context.Context, userId string, connId string) error
	RemoveConnection(ctx context.Context, userId string, connId string) (bool, error)
	GetConnections(ctx context.Context, userId string) ([]string, error)
}

func NewWsgwConnectionTracker(ctx context.Context, connectionTracking config.ConnectionTrackingConfig) (WsConnections, error) {
	switch connectionTracking.Type {
	case config.ConnectionTrackingInMemory:
		return newUserWsgwConntracker(), nil
	case config.ConnectionTrackingDynamodb:
		return NewDynamodbConntracker(ctx, connectionTracking.URL)
	case config.ConnectionTrackingValkey:
		if len(connectionTracking.URL) == 0 {
			return nil, fmt.Errorf("E2EAPP_CONNECTION_TRACKING_URL should be set when E2EAPP_CONNECTION_TRACKING=valkey")
		}
		return NewValkeyConntracker(ctx, connectionTracking.URL)
	case config.ConnectionTrackingPostgres:
		if len(connectionTracking.URL) == 0 {
			return nil, fmt.Errorf("E2EAPP_CONNECTION_TRACKING_URL should be set when E2EAPP_CONNECTION_TRACKING=postgres")
		}
		return NewPostgresConntracker(ctx, connectionTracking.URL)
	default:
		return nil, fmt.Errorf("unsupported connection tracking type '%s'", connectionTracking.Type)
	}
}
