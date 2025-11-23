package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

// PasswordCredentials holds password-credentials
type PasswordCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Config holds the available command-line options
type Config struct {
	ServerHostname        string
	ServerPort            int
	ServerURLContext      string
	SessionMaxAge         int
	LoadBalancerAddress   string
	PasswordCredentials   []PasswordCredentials
	SessionDB             string
	DBHost                string
	DBPort                int
	DBName                string
	DBUser                string
	DBPassword            string
	UsernameCookie        string
	LogLevel              string
	DynamodbURL           string
	WsgwHost              string
	WsgwPort              int
	OtlpEndpoint          string
	OtlpServiceNamespace  string
	OtlpServiceName       string
	OtlpServiceInstanceId string
}

func GetConfig(args []string) Config {
	var k = koanf.New(".")

	envNamePrefix := "E2EAPP_"
	k.Load(env.Provider(".", env.Opt{
		Prefix: envNamePrefix,
		TransformFunc: func(k, v string) (string, any) {
			// Transform the key.
			k = strings.TrimPrefix(k, envNamePrefix)

			// Transform the value into slices, if they contain spaces.
			// Eg: MYVAR_TAGS="foo bar baz" -> tags: ["foo", "bar", "baz"]
			if strings.Contains(v, " ") {
				return k, strings.Split(v, " ")
			}

			return k, v
		},
	}), nil)

	var pwdcreds []PasswordCredentials
	pwdcredsString := k.String("PASSWORD_CREDENTIALS")
	if len(pwdcredsString) > 0 {
		parseJson(pwdcredsString, &pwdcreds)
	}

	return Config{
		ServerHostname:        k.String("SERVER_HOSTNAME"),
		ServerPort:            k.Int("SERVER_PORT"),
		LoadBalancerAddress:   k.String("LOAD_BALANCER_ADDRESS"),
		PasswordCredentials:   pwdcreds,
		SessionDB:             k.String("SESSIONDB_NAME"),
		DBHost:                k.String("DB_HOST"),
		DBPort:                k.Int("DB_PORT"),
		DBName:                k.String("DB_NAME"),
		DBUser:                k.String("DB_USER"),
		DBPassword:            k.String("DB_PASSWORD"),
		UsernameCookie:        k.String("USERNAME_COOKIE"),
		LogLevel:              k.String("LOG_LEVEL"),
		DynamodbURL:           k.String("DYNAMODB_URL"),
		WsgwHost:              k.String("WSGW_HOST"),
		WsgwPort:              k.Int("WSGW_PORT"),
		OtlpEndpoint:          k.String("OTLP_ENDPOINT"),
		OtlpServiceNamespace:  k.String("OTLP_SERVICE_NAMESPACE"),
		OtlpServiceName:       k.String("OTLP_SERVICE_NAME"),
		OtlpServiceInstanceId: k.String("OTLP_SERVICE_INSTANCE_ID"),
	}
}

func parseJson(value string, parsed any) {
	unmarshalError := json.Unmarshal([]byte(value), parsed)
	if unmarshalError != nil {
		panic(fmt.Sprintf("failed to parse %s: %#v\n", value, unmarshalError))
	}
}

func GetWsgwUrl(conf Config) string {
	return fmt.Sprintf("http://%s:%d", conf.WsgwHost, conf.WsgwPort)
}
