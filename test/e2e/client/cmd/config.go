package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

// passwordCredentials holds password-credentials
type passwordCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// config holds the available command-line options
type config struct {
	ServerHostname        string
	ServerPort            int
	PasswordCredentials   []passwordCredentials
	AppServerUrl          string
	OtlpEndpoint          string
	OtlpServiceNamespace  string
	OtlpServiceName       string
	OtlpServiceInstanceId string
}

const envNamePrefix = "E2ECLIENT_"

func GetConfig(args []string) config {
	var k = koanf.New(".")

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

	var pwdcreds []passwordCredentials
	pwdcredsString := k.String("PASSWORD_CREDENTIALS")
	if len(pwdcredsString) > 0 {
		parseJson(pwdcredsString, &pwdcreds)
	}

	return config{
		ServerHostname:        k.String("SERVER_HOSTNAME"),
		ServerPort:            k.Int("SERVER_PORT"),
		PasswordCredentials:   pwdcreds, // parsed from PASSWORD_CREDENTIALS
		AppServerUrl:          k.String("APP_SERVER_URL"),
		OtlpEndpoint:          k.String("OTLP_ENDPOINT"),
		OtlpServiceNamespace:  k.String("OTLP_SERVICE_NAMESPACE"),
		OtlpServiceName:       k.String("OTLP_SERVICE_NAME"),
		OtlpServiceInstanceId: k.String("OTLP_SERVICE_INSTANCE_ID"),
	}
}

func parseJson(value string, parsed any) {
	unmarshalError := json.Unmarshal([]byte(value), parsed)
	if unmarshalError != nil {
		panic(fmt.Sprintf("failed to parse '%s': %#v\n", value, unmarshalError))
	}
}

var instanceId string
var instanceIdOnce sync.Once

func GetInstanceId() string {
	instanceIdOnce.Do(func() {
		instanceId = os.Getenv(fmt.Sprintf("%s%s", envNamePrefix, "OTLP_SERVICE_INSTANCE_ID"))
		if len(instanceId) == 0 {
			var err error
			if instanceId, err = os.Hostname(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to query hostname: %#v\n", err)
				panic(fmt.Sprintf("failed to query hostname: %v\n", err))
			}
		}
	})
	return instanceId
}

const OtelScope = "github.com/pdkovacs/wsgw/test/e2e/client"
