package ws

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/telemetry"
)

// Conn is the interface a dashboard socket must satisfy.
type Conn interface {
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// Hub manages per-tenant connections and relays Redis Pub/Sub events to them.
// One Redis subscriber is shared per tenant (reference counted) so multiple
// gateway instances each relay only channels that have active clients.
type Hub struct {
	rdb *redis.Client
	log zerolog.Logger
	m   *telemetry.Metrics

	mu      sync.Mutex
	clients map[string]map[Conn]struct{}
	subs    map[string]*tenantSub
}

type tenantSub struct {
	refCount int
	cancel   context.CancelFunc
}

func New(rdb *redis.Client, log zerolog.Logger, m *telemetry.Metrics) *Hub {
	return &Hub{
		rdb:     rdb,
		log:     log,
		m:       m,
		clients: map[string]map[Conn]struct{}{},
		subs:    map[string]*tenantSub{},
	}
}

// Register adds a connection for a tenant, starting the shared subscriber if needed.
func (h *Hub) Register(tenantID uuid.UUID, conn Conn) {
	key := tenantID.String()
	h.mu.Lock()
	if h.clients[key] == nil {
		h.clients[key] = map[Conn]struct{}{}
	}
	h.clients[key][conn] = struct{}{}

	if sub, ok := h.subs[key]; ok {
		sub.refCount++
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		h.subs[key] = &tenantSub{refCount: 1, cancel: cancel}
		go h.relay(ctx, key)
	}
	h.m.WSClients.WithLabelValues(key).Set(float64(len(h.clients[key])))
	h.mu.Unlock()

	h.log.Info().Str("tenant", key).Msg("ws client connected")
}

// Unregister removes a connection, stopping the shared subscriber when idle.
func (h *Hub) Unregister(tenantID uuid.UUID, conn Conn) {
	key := tenantID.String()
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[key]; ok {
		delete(set, conn)
		if len(set) == 0 {
			delete(h.clients, key)
		}
	}
	if sub, ok := h.subs[key]; ok {
		sub.refCount--
		if sub.refCount <= 0 {
			sub.cancel()
			delete(h.subs, key)
		}
	}
	h.m.WSClients.WithLabelValues(key).Set(float64(len(h.clients[key])))
	h.log.Info().Str("tenant", key).Msg("ws client disconnected")
}

func (h *Hub) relay(ctx context.Context, tenantKey string) {
	pubsub := h.rdb.Subscribe(ctx, cache.TenantEventsChannel(tenantKey))
	ch := pubsub.Channel()
	defer pubsub.Close()
	h.log.Info().Str("tenant", tenantKey).Msg("redis subscriber started")
	for {
		select {
		case <-ctx.Done():
			h.log.Info().Str("tenant", tenantKey).Msg("redis subscriber stopped")
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			h.broadcast(tenantKey, []byte(msg.Payload))
		}
	}
}

func (h *Hub) broadcast(tenantKey string, payload []byte) {
	h.mu.Lock()
	set := h.clients[tenantKey]
	targets := make([]Conn, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		_ = c.WriteMessage(1, payload)
	}
}

// PingLoop sends websocket pings periodically to detect dead clients.
func (h *Hub) PingLoop(conn Conn, tenantID uuid.UUID, closeFn func()) {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for range t.C {
		if err := conn.WriteMessage(1, []byte(`{"event":"ping","at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)); err != nil {
			closeFn()
			return
		}
	}
}
