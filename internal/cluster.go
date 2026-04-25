package wsgw

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
	"wsgw/internal/config"

	"github.com/rs/zerolog"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyotel"
)

// Valkey key layout
//
//   wsgw:owner:{connId}        STRING ownerIP             — fast lookup on relay path
//   wsgw:connections:{ownerIP} HASH   connId -> ""        — per-owner shard, used by the sweeper
//   wsgw:heartbeat:{ownerIP}   STRING ts (TTL)            — owner liveness
//   wsgw:sweeper:lock          STRING holderIP (TTL)      — at-most-one sweeper at a time
//
// The owner-string and the per-owner shard are written together via MULTI/EXEC on
// register/deregister. The shard exists so that the sweeper can answer "what does this
// dead owner own?" without scanning every connection record.

const (
	keyPrefix    = "wsgw:"
	keyOwner     = keyPrefix + "owner:"
	keyConnsByIP = keyPrefix + "connections:"
	keyHeartbeat = keyPrefix + "heartbeat:"
	keySweepLock = keyPrefix + "sweeper:lock"
)

// Tunables. Heartbeat TTL is 3× the heartbeat interval so a single missed write doesn't
// trip the sweeper. Sweeper interval is independent of heartbeat — it sets the upper
// bound on how quickly we discover a dead instance after its heartbeat has expired.
const (
	heartbeatInterval = 5 * time.Second
	heartbeatTTL      = 15 * time.Second
	sweepInterval     = 10 * time.Second
	sweepLockTTL      = 30 * time.Second
	relayTimeout      = 2 * time.Second
)

var errOwnerNotFound = errors.New("connection owner not found")
var errRelayFailed = errors.New("failed to relay to connection owner")

// ClusterSupport is the cluster-mode glue. When the configured Valkey URL is empty,
// NewClusterSupport returns nil and the surrounding code falls back to single-instance
// behaviour. Otherwise, Start launches the heartbeat and sweeper goroutines; Stop tears
// them down and clears this instance's heartbeat key.
type ClusterSupport struct {
	client       valkey.Client
	myIP         string
	instancePort int
	httpScheme   string
	appUrls      applicationURLs

	// Local connection registry — set by the server so the sweeper / self-relay can
	// inspect what this instance owns. Optional; nil-safe.
	localConns *wsConnections

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewClusterSupport returns nil if cluster mode is disabled. The applicationURLs and
// localConns are needed for the sweeper to fire /ws/disconnected and to short-circuit
// stale-self-relay entries.
func NewClusterSupport(conf config.Config, appUrls applicationURLs, localConns *wsConnections) *ClusterSupport {
	if len(conf.ValkeyURL) == 0 {
		return nil
	}

	myIP := os.Getenv("WSGW_INSTANCE_IPADDRESS")
	if len(myIP) == 0 {
		// We can't operate in cluster mode without a routable address for ourselves.
		// Treat as misconfiguration and fail loudly at startup.
		panic("WSGW_INSTANCE_IPADDRESS must be set when cluster mode is enabled")
	}

	scheme := os.Getenv("WSGW_INSTANCE_PROTOCOL")
	if len(scheme) == 0 {
		scheme = "http"
	}

	parsedURL, err := url.Parse(conf.ValkeyURL)
	if err != nil {
		panic(fmt.Sprintf("invalid WSGW_VALKEY_URL %q: %v", conf.ValkeyURL, err))
	}
	client, err := valkeyotel.NewClient(valkey.ClientOption{
		InitAddress: []string{parsedURL.Host},
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create valkey client for %s: %v", conf.ValkeyURL, err))
	}

	return &ClusterSupport{
		client:       client,
		myIP:         myIP,
		instancePort: conf.ServerPort,
		httpScheme:   scheme,
		appUrls:      appUrls,
		localConns:   localConns,
		stopCh:       make(chan struct{}),
	}
}

// Start launches the heartbeat and sweeper goroutines. It also writes an initial heartbeat
// synchronously so that any connections registered immediately afterwards are not seen as
// orphans by a fast-moving sweeper on a peer.
func (c *ClusterSupport) Start(ctx context.Context) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str("myIP", c.myIP).Logger()

	if err := c.writeHeartbeat(ctx); err != nil {
		logger.Error().Err(err).Msg("initial heartbeat failed")
		return err
	}

	c.wg.Add(2)
	go c.heartbeatLoop(ctx)
	go c.sweeperLoop(ctx)
	logger.Info().Msg("cluster support started")
	return nil
}

// Stop signals the goroutines to exit, waits for them, deletes this instance's heartbeat
// key (so peers detect us as gone within one sweep cycle), and closes the Valkey client.
func (c *ClusterSupport) Stop(ctx context.Context) {
	close(c.stopCh)
	c.wg.Wait()

	// Best-effort: announce departure. If Valkey is already unreachable on shutdown, the
	// TTL will clear the key on its own.
	delCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.client.Do(delCtx, c.client.B().Del().Key(keyHeartbeat+c.myIP).Build())

	c.client.Close()
}

func (c *ClusterSupport) registerConnection(ctx context.Context, connId ConnectionID) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str(ConnectionIDKey, string(connId)).Logger()

	cmds := valkey.Commands{
		c.client.B().Multi().Build(),
		c.client.B().Set().Key(keyOwner + string(connId)).Value(c.myIP).Build(),
		c.client.B().Hset().Key(keyConnsByIP + c.myIP).FieldValue().FieldValue(string(connId), "").Build(),
		c.client.B().Exec().Build(),
	}
	for _, resp := range c.client.DoMulti(ctx, cmds...) {
		if err := resp.Error(); err != nil {
			logger.Error().Err(err).Msg("registerConnection failed")
			return fmt.Errorf("registerConnection: %w", err)
		}
	}
	return nil
}

func (c *ClusterSupport) deregisterConnection(ctx context.Context, connId ConnectionID) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str(ConnectionIDKey, string(connId)).Logger()

	cmds := valkey.Commands{
		c.client.B().Multi().Build(),
		c.client.B().Del().Key(keyOwner + string(connId)).Build(),
		c.client.B().Hdel().Key(keyConnsByIP + c.myIP).Field(string(connId)).Build(),
		c.client.B().Exec().Build(),
	}
	for _, resp := range c.client.DoMulti(ctx, cmds...) {
		if err := resp.Error(); err != nil {
			logger.Error().Err(err).Msg("deregisterConnection failed")
			return fmt.Errorf("deregisterConnection: %w", err)
		}
	}
	return nil
}

// relayMessage forwards a push from a proxy instance to the owner instance. It returns:
//   errOwnerNotFound  — Valkey has no owner mapping (caller should respond 404).
//   errRelayFailed    — owner is registered but unreachable / replied non-2xx (caller
//                       should respond 502; the Valkey entry is intentionally NOT mutated,
//                       since the proxy can't tell transient unreachability from death).
//   nil               — owner accepted the relay.
//
// Self-relay short-circuit: if the lookup returns this instance's own IP, the connection
// should have been found locally. Reaching this code path means the local registry is
// inconsistent with Valkey (stale entry pointing at us). Clean up the stale entry and
// surface as not-found.
func (c *ClusterSupport) relayMessage(ctx context.Context, connId ConnectionID, message string) error {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster").Str(ConnectionIDKey, string(connId)).Logger()

	ownerIP, err := c.client.Do(ctx, c.client.B().Get().Key(keyOwner+string(connId)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return errOwnerNotFound
	}
	if err != nil {
		logger.Error().Err(err).Msg("owner lookup failed")
		return fmt.Errorf("owner lookup: %w", err)
	}

	if ownerIP == c.myIP {
		logger.Warn().Msg("stale self-owned entry — cleaning up")
		_ = c.deregisterConnection(ctx, connId)
		return errOwnerNotFound
	}

	url := fmt.Sprintf("%s://%s:%d/message/%s", c.httpScheme, ownerIP, c.instancePort, string(connId))

	relayCtx, cancel := context.WithTimeout(ctx, relayTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(relayCtx, http.MethodPost, url, bytes.NewReader([]byte(message)))
	if err != nil {
		logger.Error().Err(err).Msg("failed to build relay request")
		return fmt.Errorf("relay request build: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Error().Err(err).Str("ownerIP", ownerIP).Msg("relay HTTP call failed")
		return errRelayFailed
	}
	defer cleanupResponse(resp)

	if resp.StatusCode == http.StatusNotFound {
		// Owner is alive but no longer has this connection — propagate as not-found.
		// The owner is responsible for cleaning up its own Valkey entry; we don't.
		return errOwnerNotFound
	}
	if resp.StatusCode != http.StatusNoContent {
		logger.Error().Int("status", resp.StatusCode).Str("ownerIP", ownerIP).Msg("unexpected relay status")
		return errRelayFailed
	}
	return nil
}

func (c *ClusterSupport) heartbeatLoop(ctx context.Context) {
	defer c.wg.Done()
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster.heartbeat").Logger()

	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.writeHeartbeat(ctx); err != nil {
				logger.Warn().Err(err).Msg("heartbeat write failed")
			}
		}
	}
}

func (c *ClusterSupport) writeHeartbeat(ctx context.Context) error {
	cmd := c.client.B().Set().
		Key(keyHeartbeat + c.myIP).
		Value(strconv.FormatInt(time.Now().Unix(), 10)).
		ExSeconds(int64(heartbeatTTL.Seconds())).
		Build()
	return c.client.Do(ctx, cmd).Error()
}

func (c *ClusterSupport) sweeperLoop(ctx context.Context) {
	defer c.wg.Done()

	t := time.NewTicker(sweepInterval)
	defer t.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			c.sweepOnce(ctx)
		}
	}
}

func (c *ClusterSupport) sweepOnce(ctx context.Context) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster.sweeper").Logger()

	// At-most-one sweeper across the cluster. NX with TTL means a crashed sweeper
	// auto-releases the lock when the TTL expires.
	acquireCmd := c.client.B().Set().
		Key(keySweepLock).
		Value(c.myIP).
		Nx().
		ExSeconds(int64(sweepLockTTL.Seconds())).
		Build()
	err := c.client.Do(ctx, acquireCmd).Error()
	if valkey.IsValkeyNil(err) {
		// NX failed: another instance holds the lock.
		return
	}
	if err != nil {
		logger.Warn().Err(err).Msg("sweep lock acquire failed")
		return
	}
	defer c.releaseSweepLock(ctx)

	var cursor uint64
	for {
		scanCmd := c.client.B().Scan().Cursor(cursor).Match(keyConnsByIP + "*").Count(100).Build()
		entry, scanErr := c.client.Do(ctx, scanCmd).AsScanEntry()
		if scanErr != nil {
			logger.Error().Err(scanErr).Msg("sweep scan failed")
			return
		}
		for _, key := range entry.Elements {
			ownerIP := key[len(keyConnsByIP):]
			if ownerIP == c.myIP {
				continue
			}
			if err := c.reapIfDead(ctx, ownerIP); err != nil {
				logger.Warn().Err(err).Str("ownerIP", ownerIP).Msg("reap failed")
			}
		}
		cursor = entry.Cursor
		if cursor == 0 {
			break
		}
	}
}

// releaseSweepLock deletes the lock only if we still hold it (otherwise we'd risk
// freeing a successor sweeper's lock if our work overran the TTL). The check-and-del
// is done in a Lua script to keep it atomic.
func (c *ClusterSupport) releaseSweepLock(ctx context.Context) {
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	cmd := c.client.B().Eval().Script(script).Numkeys(1).Key(keySweepLock).Arg(c.myIP).Build()
	_ = c.client.Do(ctx, cmd).Error()
}

func (c *ClusterSupport) reapIfDead(ctx context.Context, ownerIP string) error {
	exists, err := c.client.Do(ctx, c.client.B().Exists().Key(keyHeartbeat+ownerIP).Build()).AsInt64()
	if err != nil {
		return fmt.Errorf("heartbeat exists: %w", err)
	}
	if exists == 1 {
		return nil
	}

	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster.sweeper").Str("ownerIP", ownerIP).Logger()
	logger.Info().Msg("owner declared dead — reaping connections")

	connIds, err := c.client.Do(ctx, c.client.B().Hkeys().Key(keyConnsByIP+ownerIP).Build()).AsStrSlice()
	if err != nil {
		return fmt.Errorf("hkeys: %w", err)
	}

	for _, idStr := range connIds {
		c.notifyDisconnected(ctx, ConnectionID(idStr))
		if err := c.client.Do(ctx, c.client.B().Del().Key(keyOwner+idStr).Build()).Error(); err != nil {
			logger.Warn().Err(err).Str(ConnectionIDKey, idStr).Msg("failed to delete owner key")
		}
	}
	if err := c.client.Do(ctx, c.client.B().Del().Key(keyConnsByIP+ownerIP).Build()).Error(); err != nil {
		logger.Warn().Err(err).Msg("failed to delete connections shard")
	}
	return nil
}

// notifyDisconnected fires POST /ws/disconnected for a connection whose owner died.
// Best-effort: if the backend rejects or times out, we still proceed with the Valkey
// cleanup. The contract with backends is "treat /ws/disconnected as best-effort
// liveness, not authoritative."
func (c *ClusterSupport) notifyDisconnected(ctx context.Context, connId ConnectionID) {
	logger := zerolog.Ctx(ctx).With().Str("unit", "cluster.sweeper").Str(ConnectionIDKey, string(connId)).Logger()

	if c.appUrls == nil {
		return
	}

	notifyCtx, cancel := context.WithTimeout(ctx, relayTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, c.appUrls.disconnected(), nil)
	if err != nil {
		logger.Warn().Err(err).Msg("disconnect notify request build failed")
		return
	}
	req.Header.Set(ConnectionIDHeaderKey, string(connId))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Warn().Err(err).Msg("disconnect notify HTTP failed")
		return
	}
	defer cleanupResponse(resp)
	if resp.StatusCode != http.StatusOK {
		logger.Info().Int("status", resp.StatusCode).Msg("disconnect notify non-200")
	}
}
