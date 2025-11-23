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

func CreateDbProperties(conf Config, logger zerolog.Logger) DbConnectionProperties {
	checkDefined := func(value string, name string) {
		if value == "" {
			logger.Error().Str("prop_name", name).Msg("undefined connection property")
			panic(fmt.Sprintf("connection property undefined: %s", name))
		}
	}

	checkDefined(conf.DBHost, "DBHost")
	checkDefined(conf.DBName, "DBName")
	if conf.DBPort == 0 {
		checkDefined("", "DBPort")
	}
	checkDefined(conf.DBUser, "DBUser")
	checkDefined(conf.DBPassword, "DBPassword")

	return DbConnectionProperties{
		Host:     conf.DBHost,
		Port:     conf.DBPort,
		Database: conf.DBName,
		User:     conf.DBUser,
		Password: conf.DBPassword,
	}
}
