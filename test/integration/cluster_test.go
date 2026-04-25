package integration

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"
	"wsgw/test/mockapp"

	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
)

// nrClients is the per-test client fan-out for the multi-client cluster cases.
const nrClients = 50

type clusterSupportTestSuite struct {
	*clusterBaseTestSuite
}

func TestClusterSupportTestSuite(t *testing.T) {
	logger := logging.Get().Level(zerolog.DebugLevel).With().Str("unit", "TestClusterSupportTestSuite").Logger()
	ctx := logger.WithContext(context.Background())
	suite.Run(
		t,
		&clusterSupportTestSuite{
			clusterBaseTestSuite: NewClusterBaseTestSuite(ctx),
		},
	)
}

// TestSendAMessageToApp: a client connected to one instance can send a message that
// reaches the backend. Each instance's connection-handling path is the local one
// (cluster relay isn't on this leg), but spreading clients across instances proves
// every instance can independently host owners.
func (s *clusterSupportTestSuite) TestSendAMessageToApp() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	logger := zerolog.Ctx(ctx).With().Str("testCase", "TestSendAMessageToApp").Logger()

	s.connIdGenerator = func() wsgw.ConnectionID {
		return wsgw.CreateID(ctx)
	}

	wg := sync.WaitGroup{}
	for index := 0; index < nrClients; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			ownerIdx := index % numClusterInstances
			client := NewClient(s.wsgwervers[ownerIdx], nil)
			_, err := client.connect(ctx)
			s.NoError(err)

			connId := client.connectionId
			message := "message_" + xid.New().String()

			s.mockApp.On(mockapp.MockMethodMessageReceived, connId, toWsMessage(message))
			s.mockApp.On(mockapp.MockMethodDisconnected, connId)

			s.Len(s.mockApp.GetCalls(connId), 0)

			s.NoError(client.writeMessage(ctx, toWsMessage(message)))

			_ = client.disconnect(ctx)
			<-s.mockApp.OnDisconnect(connId)

			s.Len(s.mockApp.GetCalls(connId), 2)

			call := s.getCall(connId, 0)
			s.Equal(mockapp.MockMethodMessageReceived, call.Method)
			s.assertArguments(&call, toWsMessage(message))

			call = s.getCall(connId, 1)
			s.Equal(mockapp.MockMethodDisconnected, call.Method)
			logger.Info().Int("client", index).Int("ownerIdx", ownerIdx).Msg("client finished")
		}()
	}
	wg.Wait()
}

// TestRelayAMessageFromApp: this is the test that actually exercises cluster mode.
// Each client connects to one instance (the owner) but the backend POSTs the message
// to a *different* instance, forcing it to look up the owner in Valkey and HTTP-relay.
func (s *clusterSupportTestSuite) TestRelayAMessageFromApp() {
	ctx, cancel := context.WithTimeout(s.ctx, time.Minute)
	defer cancel()

	logger := zerolog.Ctx(ctx).With().Str("testCase", "TestRelayAMessageFromApp").Logger()

	s.connIdGenerator = func() wsgw.ConnectionID {
		return wsgw.CreateID(ctx)
	}

	wg := sync.WaitGroup{}
	var relayed atomic.Int64
	for index := 0; index < nrClients; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			ownerIdx := index % numClusterInstances
			senderIdx := s.pickPeer(ownerIdx, index/numClusterInstances)
			s.NotEqual(ownerIdx, senderIdx, "test setup error: sender must differ from owner")

			msgFromAppChan := make(chan string, 1)
			client := NewClient(s.wsgwervers[ownerIdx], msgFromAppChan)
			_, err := client.connect(ctx)
			s.NoError(err)

			connId := client.connectionId
			msgToReceive := "message_" + xid.New().String()

			s.mockApp.On(mockapp.MockMethodDisconnected, connId)
			s.Len(s.mockApp.GetCalls(connId), 0)

			s.NoError(s.mockApp.SendToClientVia(ctx, s.instanceUrl(senderIdx), connId, toWsMessage(msgToReceive)))
			relayed.Add(1)

			s.Equal(msgToReceive, <-msgFromAppChan)

			_ = client.disconnect(ctx)
			<-s.mockApp.OnDisconnect(connId)

			s.Len(s.mockApp.GetCalls(connId), 1)
			s.Equal(mockapp.MockMethodDisconnected, s.getCall(connId, 0).Method)
		}()
	}
	wg.Wait()
	logger.Info().Int64("relayed", relayed.Load()).Msg("relay test finished")
}

// TestOwnerKeyRemovedAfterDisconnect: closes the loop on cleanup. The owner key in
// Valkey must be gone after the client disconnects; otherwise we'd accumulate stale
// entries that would only get reaped via the heartbeat-TTL sweep.
func (s *clusterSupportTestSuite) TestOwnerKeyRemovedAfterDisconnect() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	connId := wsgw.CreateID(ctx)
	s.nextConnId = connId
	s.connIdGenerator = nil

	client := NewClient(s.wsgwervers[0], nil)
	_, err := client.connect(ctx)
	s.NoError(err)

	s.mockApp.On(mockapp.MockMethodDisconnected, connId)

	ownerKey := "wsgw:owner:" + string(connId)
	owner, getErr := s.valkey.Get(ownerKey)
	s.NoError(getErr)
	s.Equal(s.wsgwervers[0], owner, "owner record must point to the connecting instance")

	_ = client.disconnect(ctx)
	<-s.mockApp.OnDisconnect(connId)

	// Deregistration runs in the disconnect path; give it a moment to land in Valkey.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !s.valkey.Exists(ownerKey) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.Failf("owner key not cleaned up", "key %s still present", ownerKey)
}

// TestRelaySendReceiveMixed combines relayed pushes with client→backend sends, with
// many clients in parallel. Each client owner is on instance A, sender for that
// client's pushes is on instance B; the client→backend leg stays local on A.
func (s *clusterSupportTestSuite) TestRelaySendReceiveMixed() {
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()

	logger := zerolog.Ctx(s.ctx).With().Logger()

	s.connIdGenerator = func() wsgw.ConnectionID {
		return wsgw.CreateID(ctx)
	}

	const nrMessagesPerDirection = 25

	wg := sync.WaitGroup{}
	wg.Add(nrClients)
	for run := 0; run < nrClients; run++ {
		run := run
		go func() {
			defer wg.Done()
			start := time.Now()
			ownerIdx := run % numClusterInstances
			senderIdx := s.pickPeer(ownerIdx, run/numClusterInstances)
			s.runRelaySendReceive(ctx, logger, ownerIdx, senderIdx, nrMessagesPerDirection)
			logger.Info().Int("run", run).Dur("timeTaken", time.Since(start)).Msg("run finished")
		}()
	}
	wg.Wait()
}

func (s *clusterSupportTestSuite) runRelaySendReceive(ctx context.Context, logger zerolog.Logger, ownerIdx, senderIdx, nrOneWayMessages int) {
	msgFromAppChan := make(chan string, nrOneWayMessages)

	client := NewClient(s.wsgwervers[ownerIdx], msgFromAppChan)
	_, err := client.connect(ctx)
	s.NoError(err)

	connId := client.connectionId
	s.mockApp.On(mockapp.MockMethodDisconnected, connId)
	s.Len(s.mockApp.GetCalls(connId), 0)

	msgsToSend := generateMessages(nrOneWayMessages)
	msgsToReceive := generateMessages(nrOneWayMessages)

	wg := sync.WaitGroup{}
	wg.Add(3 * nrOneWayMessages)

	go func() {
		for msg := range msgsToReceive {
			s.NoError(s.mockApp.SendToClientVia(ctx, s.instanceUrl(senderIdx), connId, toWsMessage(msg)))
			wg.Done()
		}
	}()

	go func() {
		for msg := range msgsToSend {
			jsonMsg := toWsMessage(msg)
			s.mockApp.On(mockapp.MockMethodMessageReceived, connId, jsonMsg)
			s.NoError(client.writeMessage(ctx, jsonMsg))
			wg.Done()
		}
	}()

	msgsReceived := map[string]struct{}{}
	go func() {
		for {
			msg := <-msgFromAppChan
			msgsReceived[msg] = struct{}{}
			wg.Done()
		}
	}()

	wg.Wait()

	_ = client.disconnect(ctx)
	<-s.mockApp.OnDisconnect(connId)

	s.Equal(msgsToReceive, msgsReceived)

	msgsSent := map[string]struct{}{}
	calls := s.mockApp.GetCalls(connId)
	for _, call := range calls {
		if call.Method == mockapp.MockMethodMessageReceived {
			msg, ok := call.Arguments[0].(mockapp.MessageJSON)
			if !ok {
				panic(fmt.Sprintf("%v (%T) is not a MessageJSON", call.Arguments[0], call.Arguments[0]))
			}
			msgsSent[msg["message"]] = struct{}{}
		}
	}
	s.Equal(msgsToSend, msgsSent)
}
