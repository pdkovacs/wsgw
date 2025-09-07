package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	wsgw "wsgw/internal"
	"wsgw/internal/config"
	"wsgw/internal/logging"
)

func main() {
	logger := logging.Get().With().Str("root", "main").Logger()
	ctx := logger.WithContext(context.Background())

	var serverWanted bool = true

	for _, value := range os.Args {
		if value == "-v" || value == "--version" {
			fmt.Print(config.GetBuildInfoString())
			serverWanted = false
		}
	}

	if serverWanted {
		conf := config.GetConfig(os.Args)

		var stopServer func()
		exitc := make(chan struct{})

		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc,
			syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT)
		go func() {
			s := <-sigc
			fmt.Fprintf(os.Stderr, "Caught %v, stopping server...\n", s)
			stopServer()
			fmt.Fprintln(os.Stderr, "Server stopped")
			exitc <- struct{}{}
		}()

		app := wsgw.NewServer(
			ctx,
			conf,
			func() wsgw.ConnectionID {
				return wsgw.CreateID(ctx)
			},
		)
		errAppStart := app.SetupAndStart(func(port int, stop func()) {
			stopServer = stop
		})
		if errAppStart != nil {
			panic(errAppStart)
		}

		<-exitc
		fmt.Fprintln(os.Stderr, "Exiting...")
	}
}
