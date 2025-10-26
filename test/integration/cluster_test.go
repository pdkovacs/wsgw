package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"
	"wsgw/test/mockapp"

	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
)

const nrClients = 50

type clusterSupportTestSuite struct {
	*baseTestSuite
}

func TestClusterSupportTestSuite(t *testing.T) {
	logger := logging.Get().Level(zerolog.DebugLevel).With().Str("unit", "TestclusterSupportTestSuite").Logger()
	ctx := logger.WithContext(context.Background())
	suite.Run(
		t,
		&clusterSupportTestSuite{
			baseTestSuite: NewBaseTestSuite(ctx),
		},
	)
}

func (s *clusterSupportTestSuite) TestSendAMessageToApp() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	logger := zerolog.Ctx(ctx).With().Str("testCase", "TestSendAMessageToApp").Logger()

	wg := sync.WaitGroup{}
	for index := 0; index < nrClients; index++ {
		wg.Add(1)
		logger.Info().Int("client", index).Msg("Adding client...")
		go func() {
			s.connIdGenerator = func() wsgw.ConnectionID {
				return wsgw.CreateID(ctx)
			}

			client := NewClient(s.wsgwerver, nil)
			_, err := client.connect(ctx)
			s.NoError(err)

			connId := client.connectionId
			message := "message_" + xid.New().String()

			s.mockApp.On(mockapp.MockMethodMessageReceived, connId, toWsMessage(message))
			s.mockApp.On(mockapp.MockMethodDisconnected, connId)

			s.Len(s.mockApp.GetCalls(connId), 0)

			err = client.writeMessage(ctx, toWsMessage(message))
			s.NoError(err)

			_ = client.disconnect(ctx)
			<-s.mockApp.OnDisconnect(connId)

			s.Len(s.mockApp.GetCalls(connId), 2)

			callIndex := 0
			call := s.getCall(connId, callIndex)
			s.Equal(mockapp.MockMethodMessageReceived, call.Method)
			s.assertArguments(&call, toWsMessage(message))

			callIndex++
			call = s.getCall(connId, callIndex)
			s.Equal(mockapp.MockMethodDisconnected, call.Method)
			wg.Done()
			logger.Info().Int("client", index).Msg("Client finished...")
		}()
	}
	wg.Wait()
}

func (s *clusterSupportTestSuite) TestReceiveAMessageFromApp() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	logger := zerolog.Ctx(ctx).With().Str("testCase", "TestReceiveAMessageFromApp").Logger()

	wg := sync.WaitGroup{}
	for index := 0; index < nrClients; index++ {
		wg.Add(1)
		logger.Info().Int("client", index).Msg("Adding client...")
		go func() {

			s.connIdGenerator = func() wsgw.ConnectionID {
				return wsgw.CreateID(ctx)
			}

			msgFromAppChan := make(chan string)

			client := NewClient(s.wsgwerver, msgFromAppChan)
			_, err := client.connect(ctx)
			s.NoError(err)

			connId := client.connectionId
			msgToReceive := "message_" + xid.New().String()

			s.mockApp.On(mockapp.MockMethodDisconnected, connId)

			s.Len(s.mockApp.GetCalls(connId), 0)

			err = s.mockApp.SendToClient(connId, toWsMessage(msgToReceive))
			s.NoError(err)

			msgFromApp := <-msgFromAppChan
			s.Equal(msgToReceive, msgFromApp)

			_ = client.disconnect(ctx)
			<-s.mockApp.OnDisconnect(connId)

			s.Len(s.mockApp.GetCalls(connId), 1)

			callIndex := 0
			call := s.getCall(connId, callIndex)
			s.Equal(mockapp.MockMethodDisconnected, call.Method)
			wg.Done()
		}()
	}
	wg.Wait()
}

func (s *clusterSupportTestSuite) testSendReceiveMessagesFromApp(ctx context.Context, logger zerolog.Logger, nrOneWayMessages int) {
	msgFromAppChan := make(chan string, nrOneWayMessages)

	client := NewClient(s.wsgwerver, msgFromAppChan)
	_, err := client.connect(ctx)
	s.NoError(err)

	connId := client.connectionId

	s.mockApp.On(mockapp.MockMethodDisconnected, connId)

	s.Len(s.mockApp.GetCalls(connId), 0)

	msgsToSend := generateMessages(nrOneWayMessages)
	msgsToReceive := generateMessages(nrOneWayMessages)

	wg := sync.WaitGroup{}
	wg.Add(3 * nrOneWayMessages)

	sendReceiveStart := time.Now()
	go func() {
		start := time.Now()

		for msg := range msgsToReceive {
			err = s.mockApp.SendToClient(connId, toWsMessage(msg))
			wg.Done()
			s.NoError(err)
		}

		logger.Debug().Dur("timeTaken", time.Since(start)).Msg("sending to client")
	}()

	go func() {
		start := time.Now()

		for msg := range msgsToSend {
			jsonMsg := toWsMessage(msg)
			s.mockApp.On(mockapp.MockMethodMessageReceived, connId, jsonMsg)
			err = client.writeMessage(ctx, jsonMsg)
			wg.Done()
			s.NoError(err)
		}

		logger.Debug().Dur("timeTaken", time.Since(start)).Msg("sending to app")
	}()

	msgsReceived := map[string]struct{}{}
	go func() {
		start := time.Now()

		for {
			msgFromApp := <-msgFromAppChan
			logger.Debug().Str("msgFromApp", msgFromApp).Msg("client channels message it received")
			msgsReceived[msgFromApp] = struct{}{}
			wg.Done()
			logger.Debug().Dur("timeTaken", time.Since(start)).Msg("receiving from app")
		}
	}()

	wg.Wait()
	sendReceiveDuration := time.Since(sendReceiveStart)

	logger.Info().Dur("timeTaken", sendReceiveDuration).Msg("sending-receiving")

	_ = client.disconnect(ctx)
	<-s.mockApp.OnDisconnect(connId)

	s.Equal(msgsToReceive, msgsReceived)

	s.Len(s.mockApp.GetCalls(connId), nrOneWayMessages+1)

	msgsSent := map[string]struct{}{}
	for index := 0; index < nrOneWayMessages+1; index++ {
		call := s.getCall(connId, index)
		if call.Method == mockapp.MockMethodMessageReceived {
			messageArg := call.Arguments[0]
			msg, ok := messageArg.(mockapp.MessageJSON)
			if !ok {
				panic(fmt.Sprintf("%v (%T) is not a map[string]string", messageArg, messageArg))
			}
			msgsSent[msg["message"]] = struct{}{}
		}
	}

	s.Equal(msgsToSend, msgsSent)
}

func (s *clusterSupportTestSuite) TestSendReceiveMessagesFromApp() {
	logger := zerolog.Ctx(s.ctx).With().Str("method", "TestSendReceiveMessagesFromApp").Logger()
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	s.connIdGenerator = func() wsgw.ConnectionID {
		return wsgw.CreateID(ctx)
	}

	nrMessages := 50

	s.testSendReceiveMessagesFromApp(ctx, logger, nrMessages)
}

func (s *clusterSupportTestSuite) TestSendReceiveMessagesFromAppMultiClients() {
	logger := zerolog.Ctx(s.ctx).With().Str("method", "TestSendReceiveMessagesFromApp").Logger()
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	s.connIdGenerator = func() wsgw.ConnectionID {
		return wsgw.CreateID(ctx)
	}

	nrMessages := 50

	wg := sync.WaitGroup{}
	wg.Add(nrClients)
	for run := 0; run < nrClients; run++ {
		task := run
		go func() {
			start := time.Now()
			s.testSendReceiveMessagesFromApp(ctx, logger, nrMessages)
			wg.Done()
			logger.Info().Int("taskId", task).Dur("timeTaken", time.Since(start)).Msg("run time")
		}()
	}
	wg.Wait()
}
