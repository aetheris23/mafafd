package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Cache wraps a Redis client with application helpers.
type Cache struct {
	RDB *redis.Client
	Log zerolog.Logger
}

func New(ctx context.Context, host string, port int, password string, db int, poolSize int, log zerolog.Logger) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", host, port),
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     poolSize,
		MinIdleConns: 16,
	})
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Cache{RDB: rdb, Log: log}, nil
}

func (c *Cache) Close() error { return c.RDB.Close() }

// GetJSON unmarshals a cached value, returning false when absent.
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	raw, err := c.RDB.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, err
	}
	return true, nil
}

// SetJSON stores a value with a TTL.
func (c *Cache) SetJSON(ctx context.Context, key string, val any, ttl time.Duration) error {
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return c.RDB.Set(ctx, key, b, ttl).Err()
}

// ---------------------------------------------------------------------------
// Sliding window counter (velocity checks)
// ---------------------------------------------------------------------------

const slidingWindowLua = `
local key = KEYS[1]
local member = ARGV[1]
local now = tonumber(ARGV[2])
local window = tonumber(ARGV[3])
local expire = tonumber(ARGV[4])
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local added = redis.call('ZADD', key, now, member)
local total = redis.call('ZCARD', key)
redis.call('PEXPIRE', key, expire)
return {total, added}
`

// SlidingWindowAdd records a member and returns the count within the window.
// The counter window is [now-window, now]; members are unique per event.
func (c *Cache) SlidingWindowAdd(ctx context.Context, key, member string, now time.Time, window time.Duration) (int64, error) {
	res, err := c.RDB.Eval(ctx, slidingWindowLua, []string{key},
		member, now.UnixMilli()/1000, int64(window.Seconds()), int64((window + time.Minute).Seconds())).Result()
	if err != nil {
		return 0, fmt.Errorf("sliding window: %w", err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) == 0 {
		return 0, fmt.Errorf("unexpected sliding window result")
	}
	if n, ok := arr[0].(int64); ok {
		return n, nil
	}
	return 0, fmt.Errorf("unexpected sliding window count type")
}

// ---------------------------------------------------------------------------
// Token bucket rate limiter (per tenant isolation)
// ---------------------------------------------------------------------------

const tokenBucketLua = `
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
if tokens == nil then tokens = burst end
local ts = tonumber(data[2])
if ts == nil then ts = now end
local elapsed = math.max(0, (now - ts) / 60000)
tokens = math.min(burst, tokens + elapsed * rate)
if tokens < 1 then
  redis.call('HMSET', key, 'tokens', tokens, 'ts', now)
  redis.call('PEXPIRE', key, 120000)
  local wait = math.ceil((1 - tokens) / rate * 60000)
  return {0, tokens, wait}
end
tokens = tokens - 1
redis.call('HMSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, 120000)
return {1, tokens, 0}
`

// RateLimitResult reports whether a token was consumed.
type RateLimitResult struct {
	Allowed        bool
	Remaining      float64
	RetryAfterMs   int64
}

// TokenBucketConsume atomically consumes a token at `rate` tokens/min up to `burst`.
func (c *Cache) TokenBucketConsume(ctx context.Context, key string, rate, burst float64, now time.Time) (RateLimitResult, error) {
	res, err := c.RDB.Eval(ctx, tokenBucketLua, []string{key},
		rate, burst, now.UnixMilli()).Result()
	if err != nil {
		return RateLimitResult{}, fmt.Errorf("token bucket: %w", err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) < 3 {
		return RateLimitResult{}, fmt.Errorf("unexpected token bucket result")
	}
	allowed, _ := arr[0].(int64)
	remaining, _ := arr[1].(float64)
	retry, _ := arr[2].(int64)
	return RateLimitResult{Allowed: allowed == 1, Remaining: remaining, RetryAfterMs: retry}, nil
}

// ---------------------------------------------------------------------------
// Pub/Sub
// ---------------------------------------------------------------------------

func (c *Cache) Publish(ctx context.Context, channel string, msg []byte) error {
	return c.RDB.Publish(ctx, channel, msg).Err()
}

func (c *Cache) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return c.RDB.Subscribe(ctx, channels...)
}

// Key helpers

func TenantEventsChannel(tenantID string) string { return "events:tenant:" + tenantID }
func TenantRulesKey(tenantID string) string      { return "tenant:rules:" + tenantID }
func TenantAPIKeyKey(tenantID string) string     { return "tenant:apikey:" + tenantID }
