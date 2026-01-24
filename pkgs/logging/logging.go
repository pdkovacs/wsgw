package logging

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
	"time"
	"wsgw/internal/config"

	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

type LogLevel = string

const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
)

type LogFormat = string

const (
	JSONFormat    LogFormat = "json"
	ColoredFormat LogFormat = "colored"
)

func parseLevel() zerolog.Level {
	logLevel := os.Getenv("LOG_LEVEL")
	var level zerolog.Level
	switch logLevel {
	case "info":
		level = zerolog.InfoLevel
	case "debug":
		level = zerolog.DebugLevel
	default:
		level = zerolog.InfoLevel
	}
	fmt.Printf("Log level: %v\n", level)
	return level
}

var once sync.Once

var log zerolog.Logger

func Get() zerolog.Logger {
	once.Do(func() {
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
		zerolog.TimeFieldFormat = time.RFC3339Nano

		var output io.Writer = os.Stdout

		isDevelopmentEnv := func() bool {
			return os.Getenv("APP_ENV") == "development"
		}

		var gitRevision string

		buildInfo, ok := debug.ReadBuildInfo()
		if ok {
			for _, v := range buildInfo.Settings {
				if v.Key == "vcs.revision" {
					gitRevision = v.Value
					break
				}
			}
		}

		logLevel := parseLevel()

		fmt.Fprintf(os.Stderr, "default log-level: %v\n", logLevel)

		logContext := zerolog.New(output).
			Level(zerolog.Level(logLevel)).
			With().
			Timestamp().
			Str("instanceId", config.GetInstanceId()).
			Str("git_revision", gitRevision).
			Str("go_version", buildInfo.GoVersion).
			Str("app_xid", xid.New().String())
		if isDevelopmentEnv() {
			logContext = logContext.Caller()
		}

		log = logContext.Logger()
	})

	return log
}
