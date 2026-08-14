package redisstreams

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const streamPrefix = "mafafd:stream:"

// RedisStreams implements a durable queue on Redis Streams (local-dev fallback
// for RabbitMQ). Provides the same routing-key topology used in production.
type RedisStreams struct {
	rdb *redis.Client
	log zerolog.Logger
}

func New(rdb *redis.Client, log zerolog.Logger) (*RedisStreams, error) {
	return &RedisStreams{rdb: rdb, log: log}, nil
}

func streamKey(routingKey string) string { return streamPrefix + routingKey }

func (r *RedisStreams) Publish(ctx context.Context, routingKey string, body []byte) error {
	if err := r.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey(routingKey),
		Values: map[string]any{"data": string(body)},
	}).Err(); err != nil {
		return fmt.Errorf("xadd %s: %w", routingKey, err)
	}
	return nil
}

func (r *RedisStreams) Consume(ctx context.Context, routingKey, consumerName string, concurrency int, handler func(ctx context.Context, body []byte) error) error {
	stream := streamKey(routingKey)
	group := "mafafd-group"
	// ensureGroup creates the stream + consumer group. Called at startup and
	// again whenever XREADGROUP reports NOGROUP (e.g. the stream was trimmed
	// or deleted out from under a running consumer).
	ensureGroup := func() {
		if err := r.rdb.XGroupCreateMkStream(ctx, stream, group, "$").Err(); err != nil {
			if err != redis.Nil && !isBusyGroup(err) {
				r.log.Error().Err(err).Str("stream", stream).Msg("xgroup create failed")
			}
		}
	}
	ensureGroup()
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			consumer := fmt.Sprintf("%s-%d", consumerName, id)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Recover abandoned pending messages from crashed consumers.
				if claimed, _, err := r.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
					Stream:   stream,
					Group:    group,
					MinIdle:  15 * time.Second,
					Consumer: consumer,
					Count:    16,
					Start:    "0-0",
				}).Result(); err == nil {
					for _, m := range claimed {
						r.process(ctx, stream, group, m, handler, consumer)
					}
				}
				streams, err := r.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    group,
					Consumer: consumer,
					Streams:  []string{stream, ">"},
					Count:    16,
					Block:    1000 * time.Millisecond,
				}).Result()
				if err != nil {
					if err == redis.Nil {
						continue
					}
					if strings.Contains(err.Error(), "NOGROUP") {
						ensureGroup()
						time.Sleep(50 * time.Millisecond)
						continue
					}
					r.log.Error().Err(err).Str("stream", stream).Msg("xreadgroup failed")
					time.Sleep(200 * time.Millisecond)
					continue
				}
				for _, s := range streams {
					for _, m := range s.Messages {
						r.process(ctx, stream, group, m, handler, consumer)
					}
				}
			}
		}(i)
	}
	return nil
}

func (r *RedisStreams) process(ctx context.Context, stream, group string, m redis.XMessage, handler func(ctx context.Context, body []byte) error, consumer string) {
	val, ok := m.Values["data"].(string)
	if !ok {
		_ = r.rdb.XAck(ctx, stream, group, m.ID).Err()
		return
	}
	if err := handler(ctx, []byte(val)); err != nil {
		r.log.Error().Err(err).Str("stream", stream).Str("id", m.ID).Msg("handler failed; message left pending for retry")
		return // do not ack -> XAUTOClaim will retry later
	}
	if err := r.rdb.XAck(ctx, stream, group, m.ID).Err(); err != nil {
		r.log.Error().Err(err).Str("id", m.ID).Msg("xack failed")
	}
}

func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

func (r *RedisStreams) Healthy(ctx context.Context) error {
	return r.rdb.Ping(ctx).Err()
}

func (r *RedisStreams) Depth(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for _, key := range []string{"transaction.new", "ai.analysis"} {
		// Report the number of delivered-but-unacknowledged (pending) messages,
		// which is the meaningful backlog signal for autoscaling.
		groups, err := r.rdb.XInfoGroups(ctx, streamKey(key)).Result()
		if err != nil {
			if err == redis.Nil {
				out[key] = 0
				continue
			}
			return nil, err
		}
		var pending int64
		for _, g := range groups {
			pending += g.Pending
		}
		out[key] = pending
	}
	return out, nil
}

func (r *RedisStreams) Close() error { return nil }
