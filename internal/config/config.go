package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	ServerHost            string
	ServerPort            int
	AppBaseUrl            string
	AckNewConnWithConnId  bool
	LoadBalancerAddress   string // TODO: remove this
	RedisHost             string
	RedisPort             int
	OtlpEndpoint          string
	OtlpServiceNamespace  string
	OtlpServiceName       string
	OtlpServiceInstanceId string
}

const envNamePrefix = "WSGW_"

func GetConfig(args []string) Config {
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
	return Config{
		ServerHost:            k.String("SERVER_HOST"),
		ServerPort:            k.Int("SERVER_PORT"),
		AppBaseUrl:            k.String("APP_BASE_URL"),
		AckNewConnWithConnId:  k.Bool("ACK_NEW_CONN_WITH_CONN_ID"),
		LoadBalancerAddress:   k.String("LOAD_BALANCER_ADDRESS"),
		OtlpEndpoint:          k.String("OTLP_ENDPOINT"),
		OtlpServiceNamespace:  k.String("OTLP_SERVICE_NAMESPACE"),
		OtlpServiceName:       k.String("OTLP_SERVICE_NAME"),
		OtlpServiceInstanceId: k.String("OTLP_SERVICE_INSTANCE_ID"),
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

const OtelScope = "github.com/pdkovacs/wsgw/-"
