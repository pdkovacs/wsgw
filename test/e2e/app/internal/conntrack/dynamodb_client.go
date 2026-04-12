package conntrack

import (
	"context"
	"errors"
	"log"
	nethttp "net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_config "github.com/aws/aws-sdk-go-v2/config"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	aws_dyndb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// dynamodbMaxIdleConnsPerHost caps the HTTP/1.1 connection pool for DynamoDB.
// It must be at least as large as the peak number of concurrent DynamoDB callers
// (one goroutine per Gin request, so this correlates with concurrent WebSocket users).
// Without a sufficiently large pool, closed connections accumulate in TIME_WAIT and
// exhaust the OS ephemeral port range under load.
//
// TODO: read from config (e.g. E2EAPP_DYNAMODB_MAX_IDLE_CONNS) instead of hard-wiring.
//
// TODO: for production use against real AWS DynamoDB (HTTPS), prefer HTTP/2: a single
// connection multiplexes all concurrent requests via streams, eliminating the port
// exhaustion problem entirely. Go's http.Transport negotiates h2 via ALPN automatically
// over TLS, so it may already work — verify and document. For dynamodb-local (plain HTTP),
// h2c support is unlikely, so HTTP/1.1 pooling remains the only practical option there.
const dynamodbMaxIdleConnsPerHost = 512

var errNotFound = errors.New("not found")

func createAwsConfig(ctx context.Context, dynamodbUrl string) (aws.Config, error) {

	loadOptions := []func(*aws_config.LoadOptions) error{}

	loadOptions = append(loadOptions, aws_config.WithRegion("eu-west-2"))

	if profile := os.Getenv("AWS_PROFILE"); len(profile) > 0 {
		loadOptions = append(loadOptions, aws_config.WithSharedConfigProfile(profile))
	}

	if len(dynamodbUrl) > 1 {
		loadOptions = append(loadOptions, aws_config.WithBaseEndpoint(dynamodbUrl))
	}

	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(tr *nethttp.Transport) {
		tr.MaxIdleConnsPerHost = dynamodbMaxIdleConnsPerHost
		tr.MaxIdleConns = dynamodbMaxIdleConnsPerHost
	})
	loadOptions = append(loadOptions, aws_config.WithHTTPClient(httpClient))

	cfg, err := aws_config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	otelaws.AppendMiddlewares(&cfg.APIOptions)

	return cfg, nil
}

func newDynamodbClient(ctx context.Context, dynamodbUrl string) (*aws_dyndb.Client, error) {
	awsConf, err := createAwsConfig(ctx, dynamodbUrl)
	if err != nil {
		return nil, err
	}
	svc := aws_dyndb.NewFromConfig(awsConf)
	return svc, nil
}

func Unwrap(ctx context.Context, err error) error {
	logger := zerolog.Ctx(ctx)
	var oe *smithy.OperationError
	if errors.As(err, &oe) {
		logger.Error().Str("service", oe.Service()).Str("operation", oe.Operation()).Err(oe.Unwrap()).Msgf("failed to call service: %s", oe.Service())

		tmpErr := &types.ResourceNotFoundException{}
		if errors.As(oe.Err, &tmpErr) {
			return errNotFound
		}
	}
	return err
}
