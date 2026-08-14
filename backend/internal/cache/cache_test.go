package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newClient(t *testing.T) *Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &Cache{RDB: rdb}
}

func TestTokenBucketAllowsThenRejects(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	now := time.Now()

	for i := 0; i < 5; i++ {
		res, err := c.TokenBucketConsume(ctx, "k1", 10, 5, now)
		if err != nil {
			t.Fatal(err)
		}
		if i < 5 && !res.Allowed {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	// burst of 5 exhausted -> reject
	res, err := c.TokenBucketConsume(ctx, "k1", 10, 5, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("expected rejection after burst consumed")
	}
	if res.RetryAfterMs <= 0 {
		t.Fatalf("expected retry-after, got %d", res.RetryAfterMs)
	}

	// after ~30s of refill at 10/min -> ~5 tokens refilled
	refilled := now.Add(30 * time.Second)
	res2, err := c.TokenBucketConsume(ctx, "k1", 10, 5, refilled)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Allowed {
		t.Fatal("expected token after refill period")
	}
}

func TestTokenBucketPerKeyIsolation(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	now := time.Now()
	if _, err := c.TokenBucketConsume(ctx, "tenantA", 10, 2, now); err != nil {
		t.Fatal(err)
	}
	// tenant B unaffected
	res, err := c.TokenBucketConsume(ctx, "tenantB", 10, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allowed {
		t.Fatal("tenant isolation broken")
	}
}

func TestSlidingWindowCount(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	base := time.Now()

	n1, err := c.SlidingWindowAdd(ctx, "vel:1", "txn-1", base, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 {
		t.Fatalf("count after first add = %d", n1)
	}
	n2, _ := c.SlidingWindowAdd(ctx, "vel:1", "txn-2", base.Add(9*time.Second), 10*time.Second)
	if n2 != 2 {
		t.Fatalf("count after second add = %d", n2)
	}
	// Adding at +15s with a 10s window prunes members older than 5s,
	// so txn-1 (at 0s) drops out but txn-2 (at 9s) remains.
	n3, err := c.SlidingWindowAdd(ctx, "vel:1", "txn-3", base.Add(15*time.Second), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 2 {
		t.Fatalf("expected window to prune old members, got %d", n3)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	type sample struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	in := sample{A: 7, B: "x"}
	if err := c.SetJSON(ctx, "cfg", in, time.Minute); err != nil {
		t.Fatal(err)
	}
	var out sample
	found, err := c.GetJSON(ctx, "cfg", &out)
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if out != in {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}
