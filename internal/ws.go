package wsgw

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"wsgw/internal/logging"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"nhooyr.io/websocket"
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
		connectionMessageBuffer: 16,
		wsMap:                   make(map[ConnectionID]*connection),
		logger:                  logging.Get().With().Str("unit", "notification-server").Logger(),
	}

	return ns
}

type wsIO interface {
	Close() error
	Write(ctx context.Context, msg string) error
	Read(ctx context.Context) (string, error)
}

type onMgsReceivedFunc func(c context.Context, msg string) error

func (wsconn *wsConnections) processMessages(
	ctx context.Context,
	connId ConnectionID,
	wsIo wsIO,
	onMessageFromClient onMgsReceivedFunc,
) error {
	logger := zerolog.Ctx(ctx).With().Str("method", "processMessages").Str(ConnectionIDKey, string(connId)).Logger()
	conn := newConnection(connId, wsIo, wsconn.connectionMessageBuffer)

	wsconn.addConnection(conn)
	logger.Debug().Msg("connection added")
	defer func() {
		wsconn.deleteConnection(conn)
		logger.Debug().Msg("connection removed")
	}()

	go func() {
		for {
			msgRead, errRead := wsIo.Read(ctx)
			if errRead != nil {
				logger.Debug().Msgf("Read error: %v", errRead)
				var closeError websocket.CloseError
				if errors.As(errRead, &closeError) {
					logger.Debug().Err(errRead).Msg("WS connection closing...")
					conn.connClosed <- closeError
					return
				}
				logger.Error().Err(errRead).Msg("WS connection not closing")
				select {
				case conn.fromClient <- "asdfasdf":
				default:
					go wsIo.Close()
				}
				return
			}
			conn.fromClient <- msgRead
		}
	}()

	for {
		logger.Debug().Msg("about to enter select...")
		select {
		case msg := <-conn.fromApp:
			logger.Debug().Msg("select: msg from backend")
			err := writeTimeout(ctx, time.Second*5, wsIo, msg)
			if err != nil {
				logger.Error().Err(err).Msg("select: failed to relay message from app to client")
				return err
			}
		case msg := <-conn.fromClient:
			logger.Debug().Msg("select: msg from client")
			sendToAppErr := onMessageFromClient(ctx, msg)
			if sendToAppErr != nil {
				conn.fromApp <- sendToAppErr.Error()
			}
		case closeError := <-conn.connClosed:
			logger.Debug().Err(closeError).Msg("select: ws connection closing...")
			if closeError.Code == websocket.StatusNormalClosure {
				return nil
			}
			logger.Error().Err(closeError).Msg("select: socket closed abnormaly")
			return fmt.Errorf("select: socket closed abnormaly: %w", closeError)
		case <-ctx.Done():
			logger.Debug().Msg("select: context is done")
			return ctx.Err()
		}
		logger.Debug().Msg("exited select")
	}
}

// addConnection registers a subscriber.
func (wsconn *wsConnections) addConnection(conn *connection) {
	wsconn.wsMapMux.Lock()
	defer wsconn.wsMapMux.Unlock()
	wsconn.wsMap[conn.id] = conn
}

// deleteConnection deletes the given subscriber.
func (wsconn *wsConnections) deleteConnection(conn *connection) {
	wsconn.wsMapMux.Lock()
	defer wsconn.wsMapMux.Unlock()
	defer delete(wsconn.wsMap, conn.id)
}

// It never blocks and so messages to slow subscribers
// are dropped.
func (wsconn *wsConnections) push(ctx context.Context, msg string, connId ConnectionID) error {
	conn, connNotFoundErr := wsconn.getConnection(connId)
	if connNotFoundErr != nil {
		return connNotFoundErr
	}

	conn.publishLimiter.Wait(ctx)
	conn.fromApp <- string(msg)

	return nil
}

func (wsconn *wsConnections) getConnection(connId ConnectionID) (*connection, error) {
	wsconn.wsMapMux.Lock()
	defer wsconn.wsMapMux.Unlock()
	conn, ok := wsconn.wsMap[connId]
	if !ok {
		return nil, errConnectionNotFound
	}
	return conn, nil
}

func writeTimeout(ctx context.Context, timeout time.Duration, sIo wsIO, msg string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return sIo.Write(ctx, msg)
}
