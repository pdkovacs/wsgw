package integration

import (
	"context"
	"net/http"
	"testing"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"
	"wsgw/test/mockapp"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
)

type connectingTestSuite struct {
	*baseTestSuite
}

func TestConnectingTestSuite(t *testing.T) {
	logger := logging.Get().Level(zerolog.DebugLevel).With().Str("unit", "TestConnectingTestSuite").Logger()
	ctx := logger.WithContext(context.Background())
	suite.Run(
		t,
		&connectingTestSuite{
			baseTestSuite: NewBaseTestSuite(ctx),
		},
	)
}

func (s *connectingTestSuite) TestConnectionID() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	connId := wsgw.CreateID(ctx)
	s.nextConnId = connId

	client := NewClient(s.wsgwerver, nil)

	message := toWsMessage("hi")

	s.mockApp.ExpectConnDisconn(connId)
	s.mockApp.On(mockapp.MockMethodMessageReceived, connId, message)

	s.Len(s.mockApp.GetCalls(connId), 0)

	_, err := client.connect(ctx)
	s.NoError(err)
	if err != nil {
		return
	}
	defer func() {
		client.disconnect(ctx)
		<-s.mockApp.OnDisconnect(connId)
	}()

	callIndex := 0
	s.Len(s.mockApp.GetCalls(connId), callIndex+1)
	call := s.getCall(connId, callIndex)

	err = client.writeMessage(ctx, message)
	s.NoError(err)
	s.Equal(mockapp.MockMethodConnect, call.Method)
}

func (s *connectingTestSuite) TestConnectingWithInvalidCredentials() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	connId := wsgw.CreateID(ctx)
	s.nextConnId = connId

	client := NewClient(s.wsgwerver, nil)

	response, wsConnectErr := client.connect(ctx, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{mockapp.BadCredential},
		},
	})
	s.Error(wsConnectErr)
	s.Equal(response.StatusCode, http.StatusUnauthorized)

	s.Len(s.mockApp.GetCalls(connId), 0)
}

func (s *connectingTestSuite) TestDisconnection() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	connId := wsgw.CreateID(ctx)
	s.nextConnId = connId

	client := NewClient(s.wsgwerver, nil)

	s.mockApp.ExpectConnDisconn(connId)

	s.Len(s.mockApp.GetCalls(connId), 0)

	_, err := client.connect(ctx)
	s.NoError(err)
	if err != nil {
		return
	}

	s.Equal(connId, client.connectionId)

	callIndex := 0
	s.Len(s.mockApp.GetCalls(connId), callIndex+1)
	call := s.getCall(connId, callIndex)
	s.Equal(mockapp.MockMethodConnect, call.Method)

	zerolog.Ctx(s.ctx).Debug().Msg("TestDisconnection: disconnecting")
	client.disconnect(ctx)
	zerolog.Ctx(s.ctx).Debug().Msg("TestDisconnection: waiting for disconnect ACK")
	<-s.mockApp.OnDisconnect(connId)
	zerolog.Ctx(s.ctx).Debug().Msg("TestDisconnection: disconnect ACKed")

	callIndex++
	call = s.getCall(connId, callIndex)
	s.Len(s.mockApp.GetCalls(connId), callIndex+1)
	s.Equal(mockapp.MockMethodDisconnected, call.Method)
	zerolog.Ctx(s.ctx).Debug().Msg("TestDisconnection: test finished")
}
