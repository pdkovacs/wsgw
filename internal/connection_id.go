package wsgw

import (
	"context"

	"github.com/rs/xid"
	"github.com/rs/zerolog"
)

const ConnectionIDKey = "connectionId"

type ConnectionID string

func CreateID(ctx context.Context) ConnectionID {
	logger := zerolog.Ctx(ctx)
	connectionId := xid.New().String()
	logger.Debug().Str(ConnectionIDKey, connectionId).Msg("connectionId created")
	return ConnectionID(connectionId)
}
