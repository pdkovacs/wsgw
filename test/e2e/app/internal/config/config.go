package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"wsgw/test/e2e/app/pgks/security"

	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

// Config holds the available command-line options
type Config struct {
	ServerHostname        string
	ServerPort            int
	Http2                 bool
	ServerURLContext      string
	SessionMaxAge         int
	LoadBalancerAddress   string
	PasswordCredentials   []security.PasswordCredentials
	SessionDB             string
	DBHost                string
	DBPort                int
	DBName                string
	DBUser                string
	DBPassword            string
	UsernameCookie        string
	ConnectionTracking    ConnectionTrackingConfig
	WsgwHost              string
	WsgwPort              int
	OtlpEndpoint          string
	OtlpServiceNamespace  string
	OtlpServiceName       string
	OtlpServiceInstanceId string
	OtlpTraceSampleAll    bool
}

const envNamePrefix = "E2EAPP_"

type ConnectionTrackingType string

const (
	ConnectionTrackingInMemory ConnectionTrackingType = "in-memory"
	ConnectionTrackingDynamodb ConnectionTrackingType = "dynamodb"
	ConnectionTrackingValkey   ConnectionTrackingType = "valkey"
)

type ConnectionTrackingConfig struct {
	Type ConnectionTrackingType
	URL  string
}

func (c ConnectionTrackingConfig) Validate() error {
	switch c.Type {
	case ConnectionTrackingInMemory:
		if c.URL != "" {
			return fmt.Errorf("in-memory must not have a URL")
		}
	case ConnectionTrackingDynamodb, ConnectionTrackingValkey:
		if c.URL == "" {
			return fmt.Errorf("%s requires a URL", c.Type)
		}
		u, err := url.Parse(c.URL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid connection tracking URL %q", c.URL)
		}
	default:
		return fmt.Errorf("unknown tracking type %q", c.Type)
	}
	return nil
}

func parseConnectionTracking(typeValue, urlValue string) (ConnectionTrackingConfig, error) {
	t := ConnectionTrackingType(strings.ToLower(strings.TrimSpace(typeValue)))
	if t == "" {
		t = ConnectionTrackingInMemory
	}
	cfg := ConnectionTrackingConfig{
		Type: t,
		URL:  strings.TrimSpace(urlValue),
	}
	if err := cfg.Validate(); err != nil {
		return ConnectionTrackingConfig{}, err
	}
	return cfg, nil
}

func GetConfig(args []string) (Config, error) {
	var k = koanf.New(".")

	loadErr := k.Load(env.Provider(".", env.Opt{
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
	if loadErr != nil {
		return Config{}, nil
	}

	var pwdcreds []security.PasswordCredentials
	pwdcredsString := k.String("PASSWORD_CREDENTIALS")
	if len(pwdcredsString) > 0 {
		parseErr := parseJson(pwdcredsString, &pwdcreds)
		if parseErr != nil {
			return Config{}, parseErr
		}
	}

	contrack, conntrackValidationErr := parseConnectionTracking(k.String("CONNECTION_TRACKING"), k.String("CONNECTION_TRACKING_URL"))
	if conntrackValidationErr != nil {
		return Config{}, conntrackValidationErr
	}

	return Config{
		ServerHostname:        k.String("SERVER_HOSTNAME"),
		ServerPort:            k.Int("SERVER_PORT"),
		Http2:                 k.Bool("HTTP2"),
		LoadBalancerAddress:   k.String("LOAD_BALANCER_ADDRESS"),
		PasswordCredentials:   pwdcreds, // parsed from PASSWORD_CREDENTIALS
		SessionDB:             k.String("SESSIONDB_NAME"),
		DBHost:                k.String("DB_HOST"),
		DBPort:                k.Int("DB_PORT"),
		DBName:                k.String("DB_NAME"),
		DBUser:                k.String("DB_USER"),
		DBPassword:            k.String("DB_PASSWORD"),
		UsernameCookie:        k.String("USERNAME_COOKIE"),
		ConnectionTracking:    contrack,
		WsgwHost:              k.String("WSGW_HOST"),
		WsgwPort:              k.Int("WSGW_PORT"),
		OtlpEndpoint:          k.String("OTLP_ENDPOINT"),
		OtlpServiceNamespace:  k.String("OTLP_SERVICE_NAMESPACE"),
		OtlpServiceName:       k.String("OTLP_SERVICE_NAME"),
		OtlpServiceInstanceId: k.String("OTLP_SERVICE_INSTANCE_ID"),
		OtlpTraceSampleAll:    k.Bool("OTLP_TRACE_SAMPLE_ALL"),
	}, nil
}

func parseJson(value string, parsed any) error {
	unmarshalError := json.Unmarshal([]byte(value), parsed)
	if unmarshalError != nil {
		return fmt.Errorf("failed to parse '%s': %w", value, unmarshalError)
	}
	return nil
}

func GetWsgwUrl(conf Config) string {
	return fmt.Sprintf("http://%s:%d", conf.WsgwHost, conf.WsgwPort)
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

const OtelScope = "github.com/pdkovacs/wsgw/test/e2e/app"
