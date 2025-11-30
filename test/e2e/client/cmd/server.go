package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/logging"
	"wsgw/internal/monitoring"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	metric_api "go.opentelemetry.io/otel/metric"
)

func createStartServer(serverCtx context.Context, conf config) error {
	portRequested := conf.ServerPort
	r := initEndpoints(conf)

	logger := zerolog.Ctx(serverCtx).With().Str(logging.MethodLogger, "StartServer").Logger()
	logger.Info().Msg("Starting listener....")

	lstnr, lstnrErr := net.Listen("tcp", fmt.Sprintf(":%d", portRequested))
	if lstnrErr != nil {
		panic(fmt.Sprintf("Error while starting listener at port: %v", lstnrErr))
	}

	_, port, err := net.SplitHostPort(lstnr.Addr().String())
	if err != nil {
		panic(fmt.Sprintf("Error while parsing the server address: %v", err))
	}

	logger.Info().Str("port", port).Msg("started to listen")

	server := &http.Server{
		BaseContext:  func(l net.Listener) context.Context { return serverCtx },
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	logger.Info().Msg("starting to serve...")
	srvErr := server.Serve(lstnr)
	logger.Info().Msgf("serve returned %#v", srvErr)
	return srvErr
}

func initEndpoints(conf config) *gin.Engine {
	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-client"))

	authorizedGroup := rootEngine.Group("/")
	authorizedGroup.Use(authenticationCheck(conf))
	authorizedGroup.POST("/run", func(g *gin.Context) {
		createConnectRunClients(g.Request.Context(), conf)
	})

	return rootEngine
}

func createConnectRunClients(ctx context.Context, conf config) []*Client {
	clients := []*Client{}

	sendMessage := func(msg *Message) {
		SendMessage(ctx, conf.AppServerUrl, msg)
	}

	allUserNames := []string{}
	for _, credentials := range conf.PasswordCredentials {
		cli := newClient(credentials, conf, sendMessage, createMetrics())
		clients = append(clients, cli)
		cli.connect(ctx)
		allUserNames = append(allUserNames, credentials.Username)
	}

	for _, cli := range clients {
		cli.run(allUserNames)
	}

	return clients
}

func createMetrics() *clientMonitoring {
	incMsgParseErrCounter := createCounter("ws.e2e.test.client.msg.parse.error", "incMsgParseErrCounter")
	incMsgNotFoundCounter := createCounter("ws.e2e.test.client.outstanding.msg.notfound.error", "incMsgNotFoundCounter")
	incdMsgTextMismatchCounter := createCounter("ws.e2e.test.client.outstanding.msg.text.mismatch.error", "incdMsgTextMismatchCounter")

	deliveryDurationHistogram := monitoring.CreateHistogram(OtelScope, "ws.e2e.test.deliverytime.duration", "deliveryTimeDuration", "ms")

	return &clientMonitoring{
		incMsgParseErrCounter: func(ctx context.Context) {
			incMsgParseErrCounter.Add(ctx, 1)
		},
		incOutstandingMsgNotFoundCounter: func(ctx context.Context) {
			incMsgNotFoundCounter.Add(ctx, 1)
		},
		incdMsgTextMismatchCounter: func(ctx context.Context) {
			incdMsgTextMismatchCounter.Add(ctx, 1)
		},
		recordDeliveryDuration: func(ctx context.Context, deliveryDuration time.Duration) {
			deliveryDurationHistogram.Record(ctx, float64(deliveryDuration.Milliseconds()))
		},
	}
}

func createCounter(name string, desc string) metric_api.Int64Counter {
	return monitoring.CreateCounter(OtelScope, name, desc)
}
