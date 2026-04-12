package internal

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	wsgw "wsgw/internal"
	"wsgw/pkgs/monitoring"
	"wsgw/pkgs/version_info"
	"wsgw/test/e2e/app/pgks/security"
	"wsgw/test/e2e/client/internal/config"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/memstore"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func CreateStartServer(serverCtx context.Context, conf config.Config) error {
	portRequested := conf.ServerPort
	r := initEndpoints(conf)

	logger := zerolog.Ctx(serverCtx).With().Logger()
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
		IdleTimeout:  90 * time.Second,
	}

	logger.Info().Msg("starting to serve...")
	srvErr := server.Serve(lstnr)
	logger.Info().Msgf("serve returned %#v", srvErr)
	return srvErr
}

func initEndpoints(conf config.Config) *gin.Engine {
	gob.Register(security.SessionData{})

	rootEngine := gin.Default()
	rootEngine.Use(wsgw.RequestLogger("e2etest-client"))
	rootEngine.Use(monitoring.NewOtelTraceExtraction())
	rootEngine.Use(sessions.Sessions("mysession", memstore.NewStore([]byte("secret"))))

	rootEngine.GET("/app-info", func(c *gin.Context) {
		c.JSON(200, version_info.GetVersionInfo(config.GetVersionData()))
	})

	authorizedGroup := rootEngine.Group("/")
	authorizedGroup.Use(security.AuthenticationCheck(conf.PasswordCredentials, &security.UserService{}))
	authorizedGroup.POST("/run", runTestHandler(conf))

	return rootEngine
}

func runTestHandler(conf config.Config) func(g *gin.Context) {
	var runInProgress atomic.Bool
	return func(g *gin.Context) {
		if !runInProgress.CompareAndSwap(false, true) {
			g.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		defer runInProgress.Store(false)

		runContext, cancel := context.WithCancelCause(context.WithoutCancel(g.Request.Context()))
		defer cancel(fmt.Errorf("The request handler is returning to its caller"))

		logger := zerolog.Ctx(runContext).With().Logger()

		userCount := 32
		userCountStr := g.Request.URL.Query().Get("user-count")
		if len(userCountStr) > 0 {
			var userCountConvErr error
			userCount, userCountConvErr = strconv.Atoi(userCountStr)
			if userCountConvErr != nil {
				logger.Error().Err(userCountConvErr).Str("userCountStr", userCountStr).Msg("failed to convert query parameter 'user-count'")
				g.AbortWithError(http.StatusBadRequest, userCountConvErr)
				return
			}
			if userCount < 1 {
				logger.Error().Int("userCount", userCount).Msg("'user-count' must be greater than 1")
				g.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		testDataChunkSize := 1
		testDataChunkSizeStr := g.Request.URL.Query().Get("testdata-chunksize")
		if len(testDataChunkSizeStr) > 0 {
			var testDataChunkSizeConvErr error
			testDataChunkSize, testDataChunkSizeConvErr = strconv.Atoi(testDataChunkSizeStr)
			if testDataChunkSizeConvErr != nil {
				logger.Error().Err(testDataChunkSizeConvErr).Str("testDataChunkSizeStr", testDataChunkSizeStr).Msg("failed to convert query parameter 'testdata-partion-count'")
				g.AbortWithError(http.StatusBadRequest, testDataChunkSizeConvErr)
				return
			}
			if testDataChunkSize < 1 {
				logger.Error().Int("testDataChunkSize", testDataChunkSize).Msg("'testdata-partion-count' must be greater than 1")
				g.AbortWithStatus(http.StatusBadRequest)
				return
			}
		}

		testRunTimeout := 20 * time.Minute
		testRunTimeoutStr := g.Request.URL.Query().Get("timeout")
		if len(testRunTimeoutStr) > 0 {
			var testRunTimeoutConvErr error
			testRunTimeout, testRunTimeoutConvErr = time.ParseDuration(testRunTimeoutStr)
			if testRunTimeoutConvErr != nil {
				logger.Error().Err(testRunTimeoutConvErr).Str("testRunTimeoutStr", testRunTimeoutStr).Msg("failed to convert query parameter 'timeout'")
				g.AbortWithError(http.StatusBadRequest, testRunTimeoutConvErr)
				return
			}
		}

		testRunDone := make(chan struct{})
		var notifyDoneOnce sync.Once
		notifyDone := func() {
			notifyDoneOnce.Do(func() { close(testRunDone) })
		}

		tracer := otel.Tracer(config.OtelScope)
		tmpCtx, span := tracer.Start(runContext, "test-run")
		runContext = tmpCtx
		defer span.End()

		run := newTestRun(userCount, testDataChunkSize, notifyDone, conf.Http2)
		span.SetAttributes(attribute.KeyValue{Key: "runId", Value: attribute.StringValue(run.runId)})
		runContext = logger.With().Str("runId", run.runId).Logger().WithContext(runContext)

		run.createConnectRunClients(runContext, conf)

		g.JSON(http.StatusOK, map[string]string{"id": run.runId})

		timedOut := false
		select {
		case <-testRunDone:
			logger.Debug().Bool("isContextAlive", runContext.Err() == nil).Msg("test-run done")
		case <-time.After(testRunTimeout):
			logger.Debug().Msg("had enough waiting")
			timedOut = true
		}

		endReason := "test run handler finished before all deliveries were observed"
		if timedOut {
			endReason = fmt.Sprintf("test run timed out after %s", testRunTimeout)
		}
		forceEnded := run.forceEndOutstandingMessageSpans(endReason)
		if forceEnded > 0 {
			logger.Warn().
				Int("forceEndedMessageSpanCount", forceEnded).
				Bool("timedOut", timedOut).
				Msg("force-ended outstanding request-sending-message spans")
		}
		logger.Debug().Msg("about to cancel test run context...")
	}
}
