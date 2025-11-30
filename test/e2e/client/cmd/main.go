package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
	"wsgw/test/e2e/client/internal"
	"wsgw/test/e2e/client/internal/config"

	"github.com/rs/zerolog"
)

func main() {
	ctx, cancelRequests := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGHUP,
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
	logger.Info().Any("parsed conf", conf).Send()

	monitoring.InitOtel(
		ctx,
		monitoring.OtelConfig{
			OtlpEndpoint:         conf.OtlpEndpoint,
			OtlpServiceNamespace: conf.OtlpServiceNamespace,
			OtlpServiceName:      conf.OtlpServiceName,
		},
		config.OtelScope,
	)

	srvErr := internal.CreateStartServer(ctx, conf)
	logger.Info().Msgf("server returned with %v\n", srvErr)

	<-ctx.Done()
	logger.Info().Msg("server context canceled")
	cancelRequests()
	<-time.After(time.Second * 20)
	logger.Info().Msgf("server didn't shutdown within 20 seconds\n")

	logger.Info().Msg("Exiting...")
}
