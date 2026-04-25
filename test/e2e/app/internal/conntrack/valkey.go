package conntrack

import (
	"context"
	"fmt"
	"net/url"

	"github.com/rs/zerolog"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyotel"
)

const connTrackKeyPrefix = "wsgw:conntrack:"

type ValkeyConntracker struct {
	client valkey.Client
}

func conntrackKey(userId string) string {
	return connTrackKeyPrefix + userId
}

func (v *ValkeyConntracker) AddConnection(ctx context.Context, userId string, connId string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "ValkeyConntracker").Str("userId", userId).Str("connId", connId).Logger()

	key := conntrackKey(userId)
	err := v.client.Do(ctx, v.client.B().Sadd().Key(key).Member(connId).Build()).Error()
	if err != nil {
		return fmt.Errorf("failed to add connection %s for user %s: %w", connId, userId, err)
	}

	logger.Debug().Str("key", key).Msg("connection added")
	return nil
}

func (v *ValkeyConntracker) RemoveConnection(ctx context.Context, userId string, connId string) (bool, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "ValkeyConntracker").Str("userId", userId).Str("connId", connId).Logger()

	key := conntrackKey(userId)
	n, err := v.client.Do(ctx, v.client.B().Srem().Key(key).Member(connId).Build()).AsInt64()
	if err != nil {
		return false, fmt.Errorf("failed to remove connection %s for user %s: %w", connId, userId, err)
	}

	existed := n > 0
	logger.Debug().Str("key", key).Bool("existed", existed).Msg("connection removed")
	return existed, nil
}

func (v *ValkeyConntracker) GetConnections(ctx context.Context, userId string) ([]string, error) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "ValkeyConntracker").Str("userId", userId).Logger()

	key := conntrackKey(userId)
	members, err := v.client.Do(ctx, v.client.B().Smembers().Key(key).Build()).AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("failed to get connections for user %s: %w", userId, err)
	}

	logger.Debug().Str("key", key).Int("count", len(members)).Msg("connections retrieved")
	return members, nil
}

func NewValkeyConntracker(ctx context.Context, valkeyURL string) (*ValkeyConntracker, error) {
	parsedURL, err := url.Parse(valkeyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid valkey URL %q: %w", valkeyURL, err)
	}

	addr := parsedURL.Host

	client, err := valkeyotel.NewClient(valkey.ClientOption{
		InitAddress: []string{addr},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create valkey client for %s: %w", valkeyURL, err)
	}

	zerolog.Ctx(ctx).Info().Str("addr", addr).Msg("ValkeyConntracker initialized")
	return &ValkeyConntracker{client: client}, nil
}
