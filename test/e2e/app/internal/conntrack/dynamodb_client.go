package conntrack

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_config "github.com/aws/aws-sdk-go-v2/config"
	aws_dyndb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	"github.com/rs/zerolog"
)

var errNotFound = errors.New("not found")

func createAwsConfig(dynamodbUrl string) (aws.Config, error) {

	loadOptions := []func(*aws_config.LoadOptions) error{}

	loadOptions = append(loadOptions, aws_config.WithRegion("eu-west-2"))

	if profile := os.Getenv("AWS_PROFILE"); len(profile) > 0 {
		loadOptions = append(loadOptions, aws_config.WithSharedConfigProfile(profile))
	}

	if len(dynamodbUrl) > 1 {
		loadOptions = append(loadOptions, aws_config.WithBaseEndpoint(dynamodbUrl))
	}

	cfg, err := aws_config.LoadDefaultConfig(context.TODO(), loadOptions...)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	return cfg, nil
}

func newDynamodbClient(dynamodbUrl string) (*aws_dyndb.Client, error) {
	awsConf, err := createAwsConfig(dynamodbUrl)
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
