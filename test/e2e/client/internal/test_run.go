package internal

import (
	"context"
	"fmt"
	"sync"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/client/internal/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type testRun struct {
	userCount          int
	runId              string
	outsandingMessages *pendingDeliveryTracker
	monitoring         *clientMonitoring
	clients            []*client
	notifyCompleted    func()
}

func newTestRun(userCount int, notifyCompleted func()) *testRun {
	runId := uuid.NewString()
	monitoring := createMetrics(runId)

	return &testRun{
		userCount:          userCount,
		runId:              runId,
		outsandingMessages: newMessagesById(),
		monitoring:         monitoring,
		notifyCompleted:    notifyCompleted,
	}
}

func (r *testRun) createConnectRunClients(ctx context.Context, conf config.Config) {
	testUsers := conf.PasswordCredentials
	if r.userCount > 0 {
		testUsers = conf.PasswordCredentials[:r.userCount]
	}

	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "createConnectRunClients").Logger()

	sendMessageApiUrl := fmt.Sprintf("%s%s", conf.AppServiceUrl, "/api/message")
	clients := []*client{}

	allUserNames := []string{}
	wg := sync.WaitGroup{}
	mux := sync.RWMutex{}
	for _, credentials := range testUsers {
		wg.Go(func() {
			cli := newClient(credentials, conf.WsgwUri, sendMessageApiUrl, r.outsandingMessages, r.monitoring)
			clientContext := zerolog.Ctx(ctx).With().Str("clientUser", credentials.Username).Logger().WithContext(ctx)
			cli.connectAndListen(clientContext)

			mux.Lock()
			defer mux.Unlock()
			clients = append(clients, cli)
			allUserNames = append(allUserNames, credentials.Username)
		})
	}
	wg.Wait()

	wg = sync.WaitGroup{}
	for _, cli := range clients {
		wg.Go(func() {
			cli.startTesting(ctx, r.runId, allUserNames)
		})
	}
	wg.Wait()

	logger.Debug().Msg("test for all clients started")

	r.outsandingMessages.watchDraining(r.notifyCompleted)

	r.clients = clients
}
