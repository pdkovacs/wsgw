package config

import (
	"fmt"

	"github.com/rs/zerolog"
)

type DbConnectionProperties struct {
	Host     string
	Port     int
	Database string
	Schema   string
	User     string
	Password string
}

func CreateDbProperties(options Options, logger zerolog.Logger) DbConnectionProperties {
	checkDefined := func(value string, name string) {
		if value == "" {
			logger.Error().Str("prop_name", name).Msg("undefined connection property")
			panic(fmt.Sprintf("connection property undefined: %s", name))
		}
	}

	checkDefined(options.DBHost, "DBHost")
	checkDefined(options.DBName, "DBName")
	if options.DBPort == 0 {
		checkDefined("", "DBPort")
	}
	checkDefined(options.DBUser, "DBUser")
	checkDefined(options.DBPassword, "DBPassword")

	return DbConnectionProperties{
		Host:     options.DBHost,
		Port:     options.DBPort,
		Database: options.DBName,
		User:     options.DBUser,
		Password: options.DBPassword,
	}
}
