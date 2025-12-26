package internal

import (
	"context"
	"fmt"
	"wsgw/pkgs/logging"
	"wsgw/test/e2e/client/internal/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type testRun struct {
	runId              string
	outsandingMessages *pendingDeliveriesByMsgId
	monitoring         *clientMonitoring
	clients            []*client
	notifyCompleted    func()
}

func newTestRun(notifyCompleted func()) *testRun {
	runId := uuid.NewString()
	monitoring := createMetrics(runId)

	return &testRun{
		runId:              runId,
		outsandingMessages: newMessagesById(),
		monitoring:         monitoring,
		notifyCompleted:    notifyCompleted,
	}
}

func (r *testRun) createConnectRunClients(ctx context.Context, conf config.Config) {
	logger := zerolog.Ctx(ctx).With().Str(logging.UnitLogger, "createConnectRunClients").Logger()

	sendMessageApiUrl := fmt.Sprintf("%s%s", conf.AppServiceUrl, "/api/message")
	clients := []*client{}

	allUserNames := []string{}
	for _, credentials := range conf.PasswordCredentials {
		cli := newClient(credentials, conf.WsgwUri, sendMessageApiUrl, r.outsandingMessages, r.monitoring)
		clients = append(clients, cli)
		clientContext := zerolog.Ctx(ctx).With().Str("clientUser", credentials.Username).Logger().WithContext(ctx)
		cli.connectAndListen(clientContext)
		allUserNames = append(allUserNames, credentials.Username)
	}

	for _, cli := range clients {
		cli.run(ctx, r.runId, allUserNames)
	}
	logger.Debug().Msg("finished-running-clients")

	r.outsandingMessages.watchDraining(r.notifyCompleted)

	r.clients = clients
}
