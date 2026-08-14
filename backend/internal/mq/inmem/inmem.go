package inmem

import (
	"context"
	"fmt"
	"sync"

	
)

// InMem is an in-process broker for unit tests and local debugging.
type InMem struct {
	mu     sync.Mutex
	queues map[string][]byte
	closed bool
}

func New() (*InMem, error) {
	return &InMem{queues: map[string][]byte{}}, nil
}

func (m *InMem) Publish(ctx context.Context, routingKey string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("broker closed")
	}
	m.queues[routingKey] = append(m.queues[routingKey], body...)
	return nil
}

func (m *InMem) Consume(ctx context.Context, routingKey, consumerName string, concurrency int, handler func(ctx context.Context, body []byte) error) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			m.mu.Lock()
			var msg []byte
			if q := m.queues[routingKey]; len(q) > 0 {
				msg = append([]byte(nil), q[:len(q)]...)
				m.queues[routingKey] = nil
			}
			m.mu.Unlock()
			if len(msg) > 0 {
				if err := handler(ctx, msg); err != nil {
					// requeue for retry
					m.mu.Lock()
					m.queues[routingKey] = append(m.queues[routingKey], msg...)
					m.mu.Unlock()
				}
			}
		}
	}()
	return nil
}

func (m *InMem) Healthy(ctx context.Context) error { return nil }

func (m *InMem) Depth(ctx context.Context) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int64{}
	for k, v := range m.queues {
		if len(v) > 0 {
			out[k] = 1
		}
	}
	return out, nil
}

func (m *InMem) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
