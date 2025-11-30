package internal

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/version_info"
	"wsgw/test/e2e/client/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

func CreateStartServer(serverCtx context.Context, conf config.Config) error {
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
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	logger.Info().Msg("starting to serve...")
	srvErr := server.Serve(lstnr)
	logger.Info().Msgf("serve returned %#v", srvErr)
	return srvErr
}

func initEndpoints(conf config.Config) *gin.Engine {
	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-client"))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, version_info.GetVersionInfo(config.GetVersionData()))
	})

	authorizedGroup := rootEngine.Group("/")
	authorizedGroup.Use(authenticationCheck(conf))
	authorizedGroup.POST("/run", func(g *gin.Context) {
		runContext, cancel := context.WithCancel(context.WithoutCancel(g.Request.Context()))
		testRunDone := make(chan struct{})
		notifyDone := func() {
			close(testRunDone)
		}

		tracer := otel.Tracer(config.OtelScope)
		runContext, span := tracer.Start(runContext, "test-run")
		// Shouldn't we end the span before canceling the context?
		defer span.End()

		run := newTestRun(notifyDone)
		logger := zerolog.Ctx(runContext).With().Str("runId", run.runId).Logger()
		runContext = logger.WithContext(runContext)
		run.createConnectRunClients(runContext, conf)

		carrier := http.Header{}
		propagator := propagation.TraceContext{}
		propagator.Inject(runContext, propagation.HeaderCarrier(carrier))
		for header := range carrier {
			g.Header(header, carrier[header][0])
		}

		g.JSON(http.StatusOK, map[string]string{"id": run.runId})

		// Add an attribute for demonstration
		span.SetAttributes(attribute.Bool("bool attribute", true))
		span.AddEvent("Request handled")

		select {
		case <-testRunDone:
			logger.Debug().Msg("test-run done")
		case <-time.After(60 * time.Second):
			logger.Debug().Msg("had enough waiting")
		}
		cancel()
	})

	return rootEngine
}
