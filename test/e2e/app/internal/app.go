package app

import (
	"context"
	"wsgw/test/e2e/app/internal/config"
	"wsgw/test/e2e/app/internal/httpadapter"
)

func Start(ctx context.Context, conf config.Options, getwsgw func() string, ready func(port int, stop func())) error {
	server := httpadapter.NewServer(
		conf,
		getwsgw,
	)

	server.Start(conf, func(port int, stop func()) {
		ready(port, func() {
			stop()
		})
	})

	return nil
}
