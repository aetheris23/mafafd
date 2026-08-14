package rabbit

import (
	"context"
	"fmt"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	
)

const (
	exchange    = "mafafd.events"
	dlx         = "mafafd.dlx"
	dlq         = "mafafd.dlq"
	prefetchN   = 128
)

type Rabbit struct {
	conn *amqp091.Connection
	ch   *amqp091.Channel
	log  zerolog.Logger
}

// New connects and declares the topic exchange + DLQ topology.
func New(url string, log zerolog.Logger) (*Rabbit, error) {
	conn, err := amqp091.DialConfig(url, amqp091.Config{
		Heartbeat: 30 * time.Second,
		Dial:      amqp091.DefaultDial(10 * time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("rabbit dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("rabbit channel: %w", err)
	}
	if err := ch.Qos(prefetchN, 0, false); err != nil {
		return nil, err
	}
	if err := declare(ch); err != nil {
		conn.Close()
		return nil, err
	}
	return &Rabbit{conn: conn, ch: ch, log: log}, nil
}

func declare(ch *amqp091.Channel) error {
	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	if err := ch.ExchangeDeclare(dlx, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx: %w", err)
	}
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlq: %w", err)
	}
	if err := ch.QueueBind(dlq, "#", dlx, false, nil); err != nil {
		return fmt.Errorf("bind dlq: %w", err)
	}
	// work queue bound to the topic exchange for this route
	q, err := ch.QueueDeclare("mafafd.queue.transaction.new", true, false, false, false, amqp091.Table{
		"x-dead-letter-exchange": dlx,
	})
	if err != nil {
		return fmt.Errorf("declare queue: %w", err)
	}
	if err := ch.QueueBind(q.Name, "transaction.new", exchange, false, nil); err != nil {
		return err
	}
	q, err = ch.QueueDeclare("mafafd.queue.ai.analysis", true, false, false, false, amqp091.Table{
		"x-dead-letter-exchange": dlx,
	})
	if err != nil {
		return err
	}
	return ch.QueueBind(q.Name, "ai.analysis", exchange, false, nil)
}

func (r *Rabbit) Publish(ctx context.Context, routingKey string, body []byte) error {
	err := r.ch.PublishWithContext(ctx, exchange, routingKey, true, false, amqp091.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp091.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("publish %s: %w", routingKey, err)
	}
	return nil
}

func (r *Rabbit) Consume(ctx context.Context, routingKey, consumerName string, concurrency int, handler func(ctx context.Context, body []byte) error) error {
	queueName := "mafafd.queue." + routingKey
	ch, err := r.conn.Channel()
	if err != nil {
		return err
	}
	_ = ch.Qos(prefetchN/concurrency, 0, false)
	msgs, err := ch.Consume(queueName, consumerName, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", queueName, err)
	}
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			r.log.Info().Str("queue", queueName).Str("consumer", consumerName).Int("worker", id).Msg("consumer started")
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-msgs:
					if !ok {
						return
					}
					if err := handler(ctx, msg.Body); err != nil {
						r.log.Error().Err(err).Str("routing_key", routingKey).
							Uint64("delivery_tag", msg.DeliveryTag).Msg("handler failed, requeueing")
						_ = msg.Nack(false, true)
						continue
					}
					_ = msg.Ack(false)
				}
			}
		}(i)
	}
	return nil
}

func (r *Rabbit) Healthy(ctx context.Context) error {
	if r.conn.IsClosed() {
		return fmt.Errorf("rabbit connection closed")
	}
	return nil
}

func (r *Rabbit) Depth(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for _, name := range []string{"mafafd.queue.transaction.new", "mafafd.queue.ai.analysis", dlq} {
		q, err := r.ch.QueueInspect(name)
		if err != nil {
			return nil, err
		}
		out[name] = int64(q.Messages)
	}
	return out, nil
}

func (r *Rabbit) Close() error {
	_ = r.ch.Close()
	return r.conn.Close()
}
