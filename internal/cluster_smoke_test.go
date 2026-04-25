package wsgw

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"
)

// Sanity-check that miniredis answers the commands cluster.go relies on.
// If this fails the whole cluster integration suite is dead in the water.
func TestMiniredisAcceptsClusterCommands(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{mr.Addr()},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	if err != nil {
		t.Fatalf("valkey client: %v", err)
	}
	defer c.Close()

	ctx := context.Background()

	// Plain SET/GET.
	if err := c.Do(ctx, c.B().Set().Key("k").Value("v").Build()).Error(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	if v, err := c.Do(ctx, c.B().Get().Key("k").Build()).ToString(); err != nil || v != "v" {
		t.Fatalf("GET: %q, %v", v, err)
	}

	// MULTI/EXEC like registerConnection.
	cmds := valkey.Commands{
		c.B().Multi().Build(),
		c.B().Set().Key("owner").Value("1.2.3.4:8080").Build(),
		c.B().Hset().Key("conns:1.2.3.4:8080").FieldValue().FieldValue("c1", "").Build(),
		c.B().Exec().Build(),
	}
	for i, resp := range c.DoMulti(ctx, cmds...) {
		if err := resp.Error(); err != nil {
			t.Fatalf("MULTI cmd %d: %v", i, err)
		}
	}

	// EVAL like releaseSweepLock.
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	if err := c.Do(ctx, c.B().Set().Key("lock").Value("me").Build()).Error(); err != nil {
		t.Fatalf("SET lock: %v", err)
	}
	if err := c.Do(ctx, c.B().Eval().Script(script).Numkeys(1).Key("lock").Arg("me").Build()).Error(); err != nil {
		t.Fatalf("EVAL: %v", err)
	}

	// SCAN like sweeper.
	if _, err := c.Do(ctx, c.B().Scan().Cursor(0).Match("conns:*").Count(100).Build()).AsScanEntry(); err != nil {
		t.Fatalf("SCAN: %v", err)
	}
}
