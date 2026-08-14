package mq

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/redis/go-redis/v9"

	"github.com/mafafd/backend/internal/mq/inmem"
	"github.com/mafafd/backend/internal/mq/rabbit"
	"github.com/mafafd/backend/internal/mq/redisstreams"
)

// Routing keys used across the pipeline.
const (
	RouteTransactionNew = "transaction.new" // ingestion -> rules worker
	RouteAI             = "ai.analysis"     // rules worker -> AI worker
)

// Handler processes a single message body.
type Handler = func(ctx context.Context, body []byte) error

// Broker is the durable decoupling layer between checkout and the AI engine.
type Broker interface {
	// Publish enqueues a message for a routing key.
	Publish(ctx context.Context, routingKey string, body []byte) error
	// Consume starts concurrent consumers for a routing key.
	Consume(ctx context.Context, routingKey, consumerName string, concurrency int, handler Handler) error
	// Healthy reports broker reachability.
	Healthy(ctx context.Context) error
	// Depth reports approximate queue sizes for scaling signals.
	Depth(ctx context.Context) (map[string]int64, error)
	Close() error
}

// New builds a broker for the requested provider.
func New(provider string, rabbitURL string, rdb *redis.Client, log zerolog.Logger) (Broker, error) {
	switch provider {
	case "rabbit":
		return rabbit.New(rabbitURL, log)
	case "redis":
		return redisstreams.New(rdb, log)
	case "memory":
		return inmem.New()
	default:
		return nil, fmt.Errorf("unknown MQ_PROVIDER %q (want rabbit, redis or memory)", provider)
	}
}
