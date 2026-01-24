package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	wsgw "wsgw/internal"
	"wsgw/internal/config"
	"wsgw/pkgs/logging"
	"wsgw/pkgs/monitoring"
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

	logger := logging.Get().With().Logger()
	ctx = logger.WithContext(ctx)

	var serverWanted bool = true

	for _, value := range os.Args {
		if value == "-v" || value == "--version" {
			fmt.Print(config.GetVersionData())
			serverWanted = false
		}
	}

	if serverWanted {
		conf := config.GetConfig(os.Args)

		monitoring.InitOtel(ctx, monitoring.OtelConfig{
			OtlpEndpoint:         conf.OtlpEndpoint,
			OtlpServiceNamespace: conf.OtlpServiceNamespace,
			OtlpServiceName:      conf.OtlpServiceName,
			OtlpTraceSampleAll:   conf.OtlpTraceSampleAll,
		}, config.OtelScope)

		var shutdownServer func() error

		srvErrChan := make(chan error, 1)
		ready := func(readyCtx context.Context, port int, stop func(stopCtx context.Context) error) {
			shutdownServer = func() error {
				cancelRequests()
				return stop(readyCtx)
			}
		}

		app := wsgw.NewServer(
			conf,
			func(creConnIdCtx context.Context) wsgw.ConnectionID {
				return wsgw.CreateID(creConnIdCtx)
			},
		)
		go func() {
			errAppStart := app.SetupAndStart(ctx, conf, ready)
			srvErrChan <- errAppStart
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
}
