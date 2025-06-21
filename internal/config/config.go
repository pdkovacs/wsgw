package config

import (
	"strings"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	ServerHost          string
	ServerPort          int
	AppBaseUrl          string
	LoadBalancerAddress string // TODO: remove this
	RedisHost           string
	RedisPort           int
}

func GetConfig(args []string) Config {
	var k = koanf.New(".")
	k.Load(env.Provider(".", env.Opt{
		Prefix: "WSGW_",
		TransformFunc: func(k, v string) (string, any) {
			// Transform the key.
			k = strings.TrimPrefix(k, "WSGW_")

			// Transform the value into slices, if they contain spaces.
			// Eg: MYVAR_TAGS="foo bar baz" -> tags: ["foo", "bar", "baz"]
			if strings.Contains(v, " ") {
				return k, strings.Split(v, " ")
			}

			return k, v
		},
	}), nil)
	return Config{
		ServerHost:          k.String("SERVER_HOST"),
		ServerPort:          k.Int("SERVER_PORT"),
		AppBaseUrl:          k.String("APP_BASE_URL"),
		LoadBalancerAddress: k.String("LOAD_BALANCER_ADDRESS"),
	}
}
