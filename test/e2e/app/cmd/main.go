package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
	app "wsgw/test/e2e/app/internal"
	"wsgw/test/e2e/app/internal/config"

	"github.com/rs/zerolog"
)

const (
	WSGW_ENVVAR_NAME = "WSGW"
)

func main() {
	ctx, cancelRequests := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
	defer cancelRequests()

	logger := logging.Get().Level(zerolog.GlobalLevel()).With().Str(logging.UnitLogger, "main").Logger()
	logger.Info().Msg("Test application instance starting...")
	ctx = logger.WithContext(ctx)

	conf := config.GetConfig(os.Args)
	logger.Info().Any("parsed config", conf).Send()

	monitoring.InitOtel(ctx, monitoring.OtelConfig{
		OtlpEndpoint:         conf.OtlpEndpoint,
		OtlpServiceNamespace: conf.OtlpServiceNamespace,
		OtlpServiceName:      conf.OtlpServiceName,
	}, config.OtelScope)

	var shutdownServer func() error

	srvErrChan := make(chan error, 1)
	ready := func(port int, stop func() error) {
		shutdownServer = func() error {
			cancelRequests()
			return stop()
		}
	}
	go func() {
		srvErr := app.Start(ctx, conf, getWsgw, ready)
		logger.Info().Msgf("server returned with %v\n", srvErr)
		srvErrChan <- srvErr
	}()

	<-ctx.Done()
	logger.Info().Msg("server context canceled")

	shutdownErr := shutdownServer()
	logger.Info().Msgf("server shut down with %v\n", shutdownErr)

	select {
	case srvErr := <-srvErrChan:
		logger.Info().Msgf("server exited with error: %v\n", srvErr)
	case <-time.After(time.Second * 20):
		logger.Info().Msgf("server didn't shutdown within 20 seconds\n")
	}

	logger.Info().Msg("Exiting...")
}

var getWsgw = func() string {
	wsgw := os.Getenv(WSGW_ENVVAR_NAME)
	if len(wsgw) == 0 {
		panic(fmt.Sprintf("Environment variable %s must be set", WSGW_ENVVAR_NAME))
	}
	return wsgw
}
