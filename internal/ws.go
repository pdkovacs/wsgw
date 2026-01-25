package wsgw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	loadmanagement "wsgw/pkgs/loadmanegement"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
)

type connection struct {
	fromClient chan string
	fromApp    chan string
	connClosed chan websocket.CloseError
	closeSlow  func()
	id         ConnectionID
	// publishLimiter controls the rate limit applied to the publish endpoint.
	//
	// Defaults to one publish every 100ms with a burst of 8.
	publishLimiter *rate.Limiter
}

func newConnection(connId ConnectionID, wsIo wsIO, messageBufferSize int) *connection {
	return &connection{
		id:         connId,
		fromClient: make(chan string),
		fromApp:    make(chan string, messageBufferSize),
		connClosed: make(chan websocket.CloseError),
		closeSlow: func() {
			wsIo.Close()
		},
		publishLimiter: rate.NewLimiter(rate.Every(time.Millisecond*100), 8),
	}
}

type wsConnections struct {
	connectionMessageBuffer int

	wsMapMux sync.Mutex
	wsMap    map[ConnectionID]*connection

	logger zerolog.Logger
}

var errConnectionNotFound = errors.New("connection not found")

func newWsConnections() *wsConnections {
	ns := &wsConnections{
		connectionMessageBuffer: 1024,
		wsMap:                   make(map[ConnectionID]*connection),
	}

	return ns
}

type wsIO interface {
	Close() error
	Write(ctx context.Context, msg string) error
	Read(ctx context.Context) (string, error)
}

type onMgsReceivedFunc func(c context.Context, msg string) error

func (wsconns *wsConnections) processMessages(
	ctx context.Context,
	connId ConnectionID,
	wsIo wsIO,
	onMessageFromClient onMgsReceivedFunc,
) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "wsConnections.processMessages").Str(ConnectionIDKey, string(connId)).Logger()
	conn := newConnection(connId, wsIo, wsconns.connectionMessageBuffer)

	wsconns.addConnection(conn)
	logger.Debug().Msg("connection added")
	defer func() {
		wsconns.deleteConnection(conn)
		logger.Debug().Msg("connection removed")
	}()

	go func() {
		for {
			msgRead, errRead := wsIo.Read(ctx)
			if errRead != nil {
				var closeError websocket.CloseError
				if errors.As(errRead, &closeError) {
					logger.Debug().Interface("closeError", closeError).Msg("WS connection closing...")
					conn.connClosed <- closeError
					return
				}
				logger.Error().Err(errRead).Msg("read-error, closing...")
				return
			}
			conn.fromClient <- msgRead
		}
	}()

	for {
		select {
		case msg := <-conn.fromApp:
			logger.Debug().Str("backendMsg", msg).Msg("select: msg from backend")
			err := writeWithTimeout(ctx, time.Second*5, wsIo, msg)
			if err != nil {
				logger.Error().Err(err).Msg("select: failed to relay message from app to client")
				return err
			}
		case msg := <-conn.fromClient:
			logger.Debug().Str("clientMsg", msg).Msg("select: msg from client")
			sendToAppErr := onMessageFromClient(ctx, msg)
			if sendToAppErr != nil {
				conn.fromApp <- sendToAppErr.Error()
			}
		case closeError := <-conn.connClosed:
			if closeError.Code == websocket.StatusNormalClosure {
				logger.Debug().Msg("select: StatusNormalClosure")
				return nil
			}
			if closeError.Code == websocket.StatusGoingAway {
				logger.Debug().Msg("select: socket closed with StatusGoingAway")
				return nil
			}
			logger.Error().Err(closeError).Msg("select: socket closed abnormaly")
			return fmt.Errorf("select: socket closed abnormaly: %w", closeError)
		case <-ctx.Done():
			logger.Debug().Msg("select: context is done")
			return ctx.Err()
		}
	}
}

// addConnection registers a subscriber.
func (wsconns *wsConnections) addConnection(conn *connection) {
	wsconns.wsMapMux.Lock()
	defer wsconns.wsMapMux.Unlock()
	wsconns.wsMap[conn.id] = conn
}

// deleteConnection deletes the given subscriber.
func (wsconns *wsConnections) deleteConnection(conn *connection) {
	wsconns.wsMapMux.Lock()
	defer wsconns.wsMapMux.Unlock()
	defer delete(wsconns.wsMap, conn.id)
}

// It never blocks and so messages to slow subscribers
// are dropped.
func (wsconns *wsConnections) push(_ context.Context, msg string, connId ConnectionID) error {
	conn, connNotFoundErr := wsconns.getConnection(connId)
	if connNotFoundErr != nil {
		return connNotFoundErr
	}

	// conn.publishLimiter.Wait(ctx)
	if len(conn.fromApp) >= cap(conn.fromApp) {
		return loadmanagement.OverloadError{Reason: "fromApp channel full"}
	}
	conn.fromApp <- string(msg)

	return nil
}

func (wsconns *wsConnections) getConnection(connId ConnectionID) (*connection, error) {
	wsconns.wsMapMux.Lock()
	defer wsconns.wsMapMux.Unlock()
	conn, ok := wsconns.wsMap[connId]
	if !ok {
		return nil, errConnectionNotFound
	}
	return conn, nil
}

func writeWithTimeout(ctx context.Context, timeout time.Duration, sIo wsIO, msg string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return sIo.Write(ctx, msg)
}
