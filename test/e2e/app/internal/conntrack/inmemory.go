package conntrack

import (
	"context"
	"slices"
	"sync"
	"wsgw/internal/logging"

	"github.com/rs/zerolog"
)

// TODO: persist this data in a globally accessible store to support clustered applications
type InmemoryConntracker struct {
	m  map[string][]string
	mx sync.Mutex
}

func (conntracker *InmemoryConntracker) AddConnection(ctx context.Context, userId string, connId string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "InmemoryConntracker").Str(logging.MethodLogger, "AddConnectionId").Str("userId", userId).Str("connId", connId).Logger()
	logger.Debug().Msg("BEGIN")
	conntracker.mx.Lock()
	defer conntracker.mx.Unlock()

	value, exists := conntracker.m[userId]

	if !exists {
		conntracker.m[userId] = []string{connId}
	} else {
		conntracker.m[userId] = append(value, connId)
	}

	return nil
}

func (conntracker *InmemoryConntracker) RemoveConnection(ctx context.Context, userId string, connId string) (bool, error) {
	conntracker.mx.Lock()
	defer conntracker.mx.Unlock()

	connIdList, exists := conntracker.m[userId]

	if exists {
		connIdList = slices.DeleteFunc(connIdList, func(elem string) bool {
			return elem == connId
		})
		conntracker.m[userId] = connIdList
	}

	return exists, nil
}

func (conntracker *InmemoryConntracker) GetConnections(ctx context.Context, userId string) ([]string, error) {
	conntracker.mx.Lock()
	defer conntracker.mx.Unlock()

	connIdList, exists := conntracker.m[userId]

	if exists {
		tmp := make([]string, len(connIdList))
		copy(tmp, connIdList)
		return tmp, nil
	}

	return nil, nil
}

func newUserWsgwConntracker() *InmemoryConntracker {
	connectionMap := InmemoryConntracker{
		m: map[string][]string{},
	}
	return &connectionMap
}
