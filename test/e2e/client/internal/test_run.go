package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/app/pgks/dto"
	"wsgw/test/e2e/client/internal/config"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	numCoroutines int = 16
)

var transport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConnsPerHost:   numCoroutines,
	ResponseHeaderTimeout: 90 * time.Second,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 10 * time.Second,
}

var httpClient http.Client = http.Client{
	Transport: transport,
	Timeout:   90 * time.Second,
}

type testRun struct {
	userCount            int
	testDataPartionCount int
	runId                string
	dlvrTracker          *deliveryTracker
	monitoring           *clientMonitoring
	clients              []*client
	notifyCompleted      func()
}

func newTestRun(userCount int, testDataPartionCount int, notifyCompleted func()) *testRun {
	runId := uuid.NewString()
	monitoring := createMetrics(runId)

	return &testRun{
		userCount:            userCount,
		testDataPartionCount: testDataPartionCount,
		runId:                runId,
		dlvrTracker:          newDeliveryTracker(),
		monitoring:           monitoring,
		notifyCompleted:      notifyCompleted,
	}
}

func (r *testRun) createConnectRunClients(ctx context.Context, conf config.Config) {
	testUsers := conf.PasswordCredentials
	if r.userCount > 0 {
		testUsers = conf.PasswordCredentials[:r.userCount]
	}

	logger := zerolog.Ctx(ctx).With().Logger()

	clients := []*client{}

	allUserNames := []string{}
	wg := sync.WaitGroup{}
	mux := sync.RWMutex{}
	for _, credentials := range testUsers {
		wg.Go(func() {
			cli := newClient(credentials, conf.WsgwUri, r.dlvrTracker, r.monitoring, r.runId)
			clientContext := zerolog.Ctx(ctx).With().Str("clientUser", credentials.Username).Logger().WithContext(ctx)
			cli.connectAndListen(clientContext)

			mux.Lock()
			defer mux.Unlock()
			clients = append(clients, cli)
			allUserNames = append(allUserNames, credentials.Username)
		})
	}
	wg.Wait()

	for _, cli := range clients {
		msg := cli.createMessage(ctx, allUserNames)
		r.dlvrTracker.pendingDeliveries[msg.id] = msg
	}

	sendMessageApiUrl := fmt.Sprintf("%s%s", conf.AppServiceUrl, "/api/messages-in-bulk")
	testData := r.dlvrTracker.pendingDeliveries
	r.sendTestData(ctx, testData, sendMessageApiUrl, conf.PasswordCredentials[0])
	logger.Debug().Msg("test for all clients started")

	r.dlvrTracker.watchDraining(r.notifyCompleted)

	r.clients = clients
}

func (r *testRun) sendTestData(ctx context.Context, allMessages map[string]*message, endpoint string, epCredentials config.PasswordCredentials) error {
	logger := zerolog.Ctx(ctx).With().Str("endpoint", endpoint).Logger()

	logger.Debug().Msg("BEGIN")

	messagesToSend := []dto.E2EMessage{}

	logger.Debug().Int("msgCount", len(allMessages)).Msg("sending messages in bulk")
	for _, msg := range allMessages {
		recips := []string{}
		for _, recip := range msg.recipients {
			recips = append(recips, string(recip))
		}

		msgDto := dto.E2EMessage{
			TestRunId:  msg.testRunId,
			Id:         msg.id,
			Sender:     msg.sender.Username,
			Recipients: recips,
			Data:       msg.text,
			// the test-app will basically ignore this for now, it will start a new root context for each push
			TraceData: monitoring.InjectTraceData(ctx),
		}

		messagesToSend = append(messagesToSend, msgDto)
	}

	tracer := otel.Tracer(config.OtelScope)
	for _, msg := range allMessages {
		_, msg.span = tracer.Start(
			ctx,
			"request-sending-message",
			trace.WithAttributes(attribute.String("runId", r.runId)),
		)
	}

	var errs error

	var wg sync.WaitGroup
	chunkSize := (len(messagesToSend) + r.testDataPartionCount - 1) / r.testDataPartionCount
	for c := range slices.Chunk(messagesToSend, chunkSize) {
		fmt.Printf(">>>>>>>>>>>>>>>> chunk size: %d\n", len(c))
		wg.Go(func() {
			if err := r.sendTestDataChunk(ctx, c, endpoint, epCredentials); err != nil {
				errs = errors.Join(errs, err)
			}
		})
	}
	wg.Wait()

	logger.Debug().Msg("END")
	return errs
}

func (r *testRun) sendTestDataChunk(ctx context.Context, chunkToSend []dto.E2EMessage, endpoint string, epCredentials config.PasswordCredentials) error {
	logger := zerolog.Ctx(ctx).With().Str("endpoint", endpoint).Logger()
	message, marshalErr := json.Marshal(chunkToSend)
	if marshalErr != nil {
		logger.Error().Err(marshalErr).Msg("failed to marshal message")
	}

	req, createReqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(message)))
	if createReqErr != nil {
		logger.Error().Err(createReqErr).Msg("failed to create request")
		return createReqErr
	}
	req.Header = createBasicAuthnHeader(epCredentials.Username, epCredentials.Password)

	monitoring.InjectIntoHeader(ctx, req.Header)

	response, sendReqErr := httpClient.Do(req)
	if sendReqErr != nil {
		logger.Error().Err(sendReqErr).Msgf("failed to send request: %#v", sendReqErr)
		return sendReqErr
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}()

	if response.StatusCode != http.StatusNoContent {
		logger.Error().Str("endpoint", endpoint).Int("statusCode", response.StatusCode).Msg("failed to send request")
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	return nil
}
