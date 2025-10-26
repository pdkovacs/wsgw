package conntrack

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	aws_dyndb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rs/zerolog"
)

const (
	connTrackerTableName  = "WsgwConnectionIds"
	userIdAttribute       = "UserId"
	connectionIdAttribute = "ConnectionId"
)

type DyndbWsgwConnId struct {
	UserId       string `dynamodbav:"UserId"`
	ConnectionId string `dynamodbav:"ConnectionId"`
}

func (dyConnId *DyndbWsgwConnId) GetKey(ctx context.Context) (map[string]types.AttributeValue, error) {
	userId, err := attributevalue.Marshal(dyConnId.UserId)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dynamodb attribute `%s`: %w", userIdAttribute, Unwrap(ctx, err))
	}

	connId, err := attributevalue.Marshal(dyConnId.ConnectionId)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dynamodb attribute `%s`: %w", connectionIdAttribute, Unwrap(ctx, err))
	}

	return map[string]types.AttributeValue{
		userIdAttribute:       userId,
		connectionIdAttribute: connId,
	}, nil
}

type DyndbConntracker struct {
	client *aws_dyndb.Client
}

func (connmap *DyndbConntracker) AddConnection(ctx context.Context, userId string, connId string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "DyndbConntracker").Str("method", "AddConnectionId").Str("userId", userId).Str("connId", connId).Logger()

	connIdAttr := &DyndbWsgwConnId{UserId: userId, ConnectionId: connId}

	newItem, marshalErr := attributevalue.MarshalMap(connIdAttr)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal new connectionId item for updating %v: %w", connIdAttr, marshalErr)
	}

	input := &aws_dyndb.PutItemInput{
		TableName:              aws.String(connTrackerTableName),
		Item:                   newItem,
		ReturnConsumedCapacity: "TOTAL",
	}
	output, err := connmap.client.PutItem(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create connectionId %v: %w", connIdAttr, Unwrap(ctx, err))
	}
	if logger.GetLevel() == zerolog.DebugLevel {
		logger.Debug().Interface("output", output).Send()
	}

	return nil
}

func (connmap *DyndbConntracker) RemoveConnection(ctx context.Context, userId string, connId string) (bool, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "DyndbConntracker").Str("method", "RemoveConnectionId").Str("userId", userId).Str("connId", connId).Logger()

	dynConnId := DyndbWsgwConnId{
		UserId:       userId,
		ConnectionId: connId,
	}
	pk, getKeyErr := dynConnId.GetKey(ctx)
	if getKeyErr != nil {
		return false, getKeyErr
	}

	input := &aws_dyndb.DeleteItemInput{
		TableName:              aws.String(connTrackerTableName),
		Key:                    pk,
		ReturnConsumedCapacity: "TOTAL",
	}
	output, err := connmap.client.DeleteItem(ctx, input)
	if err != nil {
		return false, fmt.Errorf("failed to delete connectionId %s::%s: %w", userId, connId, Unwrap(ctx, err))
	}

	if logger.GetLevel() == zerolog.DebugLevel {
		logger.Debug().Interface("delete-result", output).Send()
	}

	return false, nil
}

func (connmap *DyndbConntracker) GetConnections(ctx context.Context, userId string) ([]string, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "DyndbConntracker").Str("method", "RemoveConnectionGetConnectionsId").Str("userId", userId).Logger()

	var err error
	var response *aws_dyndb.QueryOutput
	var connIds []string
	keyEx := expression.Key(userIdAttribute).Equal(expression.Value(userId))
	expr, err := expression.NewBuilder().WithKeyCondition(keyEx).Build()
	if err != nil {
		logger.Error().Err(err).Msg("failed to build expression for query")
	} else {
		queryPaginator := aws_dyndb.NewQueryPaginator(connmap.client, &aws_dyndb.QueryInput{
			TableName:                 aws.String(connTrackerTableName),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
			KeyConditionExpression:    expr.KeyCondition(),
		})
		for queryPaginator.HasMorePages() {
			logger.Debug().Msg("fetching next page...")
			response, err = queryPaginator.NextPage(ctx)
			if err != nil {
				logger.Error().Err(err).Msg("failed to query for connIds")
				break
			} else {
				var connIdPage []DyndbWsgwConnId
				err = attributevalue.UnmarshalListOfMaps(response.Items, &connIdPage)
				if err != nil {
					logger.Error().Err(err).Msg("failed to unmarshal query response")
					break
				} else {
					for _, connId := range connIdPage {
						connIds = append(connIds, connId.ConnectionId)
					}
				}
			}
		}
	}
	return connIds, err
}

func NewDynamodbConntracker(dynamodbUrl string) (*DyndbConntracker, error) {
	client, err := newDynamodbClient(dynamodbUrl)
	if err != nil {
		return nil, fmt.Errorf("unable to create dynamodb client for %s: %w", dynamodbUrl, err)
	}
	tracker := DyndbConntracker{client}
	fmt.Printf(">>>>>>>>>>>>>>>>>>>> DyndbConntracker initialized with %s\n", dynamodbUrl)
	return &tracker, nil
}
