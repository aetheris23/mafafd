package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/api"
	"github.com/mafafd/backend/internal/auth"
	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/config"
	"github.com/mafafd/backend/internal/httpserver"
	"github.com/mafafd/backend/internal/seed"
	"github.com/mafafd/backend/internal/store"
	"github.com/mafafd/backend/internal/telemetry"
	"github.com/mafafd/backend/internal/ws"
)

func main() {
	cfg, err := config.Load("gateway")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := telemetry.InitLogger(cfg.Service, cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL, log)
	telemetry.FatalIfErr(err, "connect postgres")
	defer st.Close()

	cacheClient, err := cache.New(ctx, cfg.RedisHost, cfg.RedisPort, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, log)
	telemetry.FatalIfErr(err, "connect redis")
	defer cacheClient.Close()

	if cfg.SeedDemoData {
		if err := seed.Run(ctx, st, log); err != nil {
			log.Warn().Err(err).Msg("demo seeding skipped/failed")
		}
	}

	metrics := telemetry.NewMetrics()
	hub := ws.New(cacheClient.RDB, log, metrics)

	server := &api.Server{Cfg: cfg, Log: log, Store: st, Cache: cacheClient}

	f := httpserver.NewApp(log, metrics)
	f.Use(httpserver.MetricsMiddleware(metrics))
	f.Use(func(c *fiber.Ctx) error {
		c.Locals("jwt_secret", cfg.JWTSecret)
		return c.Next()
	})

	f.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "gateway", "time": time.Now()})
	})
	f.Get("/ready", func(c *fiber.Ctx) error {
		ctx2, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := st.Ping(ctx2); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "not_ready", "postgres": "down"})
		}
		if err := cacheClient.RDB.Ping(ctx2).Err(); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "not_ready", "redis": "down"})
		}
		return c.JSON(fiber.Map{"status": "ready"})
	})
	f.Get("/metrics", adaptor.HTTPHandler(metrics.HTTPHandler()))

	server.Register(f)
	registerWS(f, cfg, log, hub)

	go func() {
		if err := f.Listen(fmt.Sprintf(":%d", cfg.Port)); err != nil {
			log.Fatal().Err(err).Msg("gateway http server failed")
		}
	}()
	log.Info().Int("port", cfg.Port).Msg("gateway listening")
	<-ctx.Done()
	log.Info().Msg("shutting down gateway")
	_ = f.ShutdownWithTimeout(5 * time.Second)
}

// registerWS exposes the live event stream at /api/v1/ws.
func registerWS(f *fiber.App, cfg *config.Config, log zerolog.Logger, hub *ws.Hub) {
	f.Use("/api/v1/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	f.Get("/api/v1/ws", websocket.New(func(conn *websocket.Conn) {
		token := conn.Query("token")
		if token == "" {
			token = conn.Cookies("mafafd_token")
		}
		claims, err := auth.Parse(cfg.JWTSecret, token)
		if err != nil || claims.Scope != "ws" {
			_ = conn.WriteMessage(websocket.CloseMessage, []byte("unauthorized"))
			return
		}
		tenantID := claims.TenantID
		hub.Register(tenantID, conn)

		done := make(chan struct{})
		go func() {
			defer close(done)
			conn.SetReadLimit(1024)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					_ = conn.Close()
					return
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Hour): // session cap
			_ = conn.Close()
		}
		hub.Unregister(tenantID, conn)
	}))
}
