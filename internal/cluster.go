package wsgw

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
	"wsgw/internal/config"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const connectionHashSetName = "connections"

type KeyvalueStore struct {
	rdb *redis.Client
}

func NewKeyvalueStore(host string, port int) *KeyvalueStore {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	return &KeyvalueStore{rdb: rdb}
}

func (client *KeyvalueStore) registerConnection(ctx context.Context, connectionId ConnectionID) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str("method", "registerConnection").Str(ConnectionIDKey, string(connectionId)).Logger()
	myIpAddress, addressErr := getMyIPAddress()
	if addressErr != nil {
		logger.Error().Msg("Cannot register my connection ownership, I have no IP address")
		return addressErr
	}
	redisError := client.rdb.HSet(ctx, connectionHashSetName, connectionId, myIpAddress)
	if redisError != nil {
		logger.Error().Err(redisError.Err()).Msg("error while registering connection")
		return fmt.Errorf("connection registration error: %w", redisError.Err())
	}
	return nil
}

func (client *KeyvalueStore) deregisterConnection(ctx context.Context, connectionId ConnectionID) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str("method", "deregisterConnection").Str(ConnectionIDKey, string(connectionId)).Logger()
	logger.Debug().Send()

	redisError := client.rdb.HDel(ctx, connectionHashSetName, string(connectionId))
	if redisError != nil {
		logger.Error().Err(redisError.Err()).Msg("error while deregistering connection")
		return fmt.Errorf("connection deregistration error: %w", redisError.Err())
	}
	return nil
}

func (client *KeyvalueStore) findConnectionOwnersAddress(ctx context.Context, connectionId ConnectionID) (string, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str("method", "findConnectionOwnersAddress").Str(ConnectionIDKey, string(connectionId)).Logger()
	logger.Debug().Send()

	redisStringCmd := client.rdb.HGet(ctx, connectionHashSetName, string(connectionId))
	err := redisStringCmd.Err()
	if err != nil {
		logger.Error().Err(err).Msg("failed to retrieve connection owner's address")
		return "", fmt.Errorf("failed to retrieve connection owner's address: %w", err)
	}

	return redisStringCmd.Val(), nil
}

type ClusterSupport struct {
	kvClient *KeyvalueStore
}

func NewClusterSupport(conf config.Config) *ClusterSupport {
	if len(conf.RedisHost) == 0 {
		return nil
	}
	return &ClusterSupport{kvClient: NewKeyvalueStore(conf.RedisHost, conf.RedisPort)}
}

func (cluster *ClusterSupport) registerConnection(ctx context.Context, connectionId ConnectionID) error {
	return cluster.kvClient.registerConnection(ctx, connectionId)
}

func (cluster *ClusterSupport) deregisterConnection(ctx context.Context, connectionId ConnectionID) error {
	return cluster.kvClient.deregisterConnection(ctx, connectionId)
}

func (cluster *ClusterSupport) relayMessage(ctx context.Context, connectionId ConnectionID, message string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "clusterSupport").Str("method", "relayMessage").Str(ConnectionIDKey, connIdPathParamName).Str("message", message).Logger()
	connOwnerIpAddress, errAddress := cluster.kvClient.findConnectionOwnersAddress(ctx, connectionId)
	if errAddress != nil {
		logger.Error().Err(errAddress).Msg("failed to find connection owner's address")
		return fmt.Errorf("cannot find connection owner's address: %w", errAddress)
	}
	logger = logger.With().Str("connOwnerIpAddress", connOwnerIpAddress).Logger()

	protocol := os.Getenv("MY_INSTANCE_PROTOCOL")
	if len(protocol) == 0 {
		errMsg := "MY_INSTANCE_PROTOCOL is not set"
		logger.Error().Msg(errMsg)
		return fmt.Errorf(errMsg)
	}
	port, portErr := getInstancePort()
	if portErr != nil {
		logger.Error().Err(portErr)
		return portErr
	}

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s://%s:%s/%s", protocol, connOwnerIpAddress, port, fmt.Sprintf("/message/%s", connectionId)), nil)
	if err != nil {
		logger.Error().Msgf("failed to create request object: %v", err)
		return fmt.Errorf("failed to create request object: %w", err)
	}

	client := http.Client{
		Timeout: time.Second * 15,
	}
	response, requestErr := client.Do(request)
	if requestErr != nil {
		logger.Error().Msgf("failed to send request: %v", requestErr)
		return fmt.Errorf("failed to send request: %w", requestErr)
	}
	defer cleanupResponse(response)

	logger.Info().Msgf("Received status code %d", response.StatusCode)
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("sending message to client finished with unexpected HTTP status: %v", response.StatusCode)
	}

	return nil
}

func getMyIPAddress() (string, error) {
	myIpAddress := os.Getenv("MY_INSTANCE_IPADDRESS")
	if len(myIpAddress) == 0 {
		return "", fmt.Errorf("connection registration error: no IP address to register")
	}
	return myIpAddress, nil
}

func getInstancePort() (string, error) {
	port := os.Getenv("MY_INSTANCE_PORT")
	if len(port) == 0 {
		errMsg := "MY_INSTANCE_PORT is not set"
		return "", fmt.Errorf(errMsg)
	}
	return port, nil
}
