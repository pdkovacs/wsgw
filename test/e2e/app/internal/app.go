package app

import (
	"context"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/httpadapter"
)

func Start(serverCtx context.Context, conf config.Options, getwsgw func() string, ready func(port int, stop func() error)) error {
	server := httpadapter.NewServer(
		getwsgw,
	)

	return server.Start(serverCtx, conf, func(ctxReady context.Context, port int, stop func(ctxStop context.Context) error) {
		if ready != nil {
			ready(port, func() error {
				return stop(ctxReady)
			})
		}
	})
}
