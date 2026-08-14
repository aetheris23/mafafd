package httpserver

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"

	"github.com/mafafd/backend/internal/telemetry"
)

// NewApp builds a Fiber app with common hardened middleware.
func NewApp(log zerolog.Logger, m *telemetry.Metrics) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             32768,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		AppName:               "mafafd",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if fe, ok := err.(*fiber.Error); ok {
				return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
			}
			log.Error().Err(err).Str("path", c.Path()).Msg("unhandled error")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		},
	})
	app.Use(recover.New())
	app.Use(RequestID())
	app.Use(Logger(log))
	app.Use(SecurityHeaders())
	app.Use(RequestSizeLimit(32768))
	return app
}

// MetricsMiddleware records request count and latency.
func MetricsMiddleware(m *telemetry.Metrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		m.HTTPRequests.WithLabelValues(string(c.Method()), c.Route().Path, statusString(status)).Inc()
		m.HTTPLatency.WithLabelValues(string(c.Method()), c.Route().Path).Observe(time.Since(start).Seconds())
		return err
	}
}

func statusString(code int) string {
	return string(fasthttp.StatusMessage(code))
}

// RequestID injects a traceable request id.
func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Get(fiber.HeaderXRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Locals("request_id", id)
		c.Set(fiber.HeaderXRequestID, id)
		return c.Next()
	}
}

// Logger emits structured access logs. Debug-gated so a saturated hot path
// (e.g. 3k+ txn/s) never serializes on stdout; enable with LOG_LEVEL=debug.
func Logger(log zerolog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		log.Debug().
			Str("request_id", reqID(c)).
			Str("method", string(c.Method())).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("latency", time.Since(start)).
			Str("ip", c.IP()).
			Msg("request")
		return err
	}
}

func reqID(c *fiber.Ctx) string {
	if v, ok := c.Locals("request_id").(string); ok {
		return v
	}
	return ""
}

// SecurityHeaders sets defensive response headers.
func SecurityHeaders() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-Permitted-Cross-Domain-Policies", "none")
		c.Set("Cache-Control", "no-store")
		return c.Next()
	}
}

// RequestSizeLimit rejects oversized request bodies before parsing.
func RequestSizeLimit(n int64) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if int64(c.Request().Header.ContentLength()) > n {
			return fiber.NewError(fiber.StatusRequestEntityTooLarge, "payload too large")
		}
		return c.Next()
	}
}

// LocalInt helper to read a request-scoped value.
func LocalInt(c *fiber.Ctx, key string) int {
	if v, ok := c.Locals(key).(int); ok {
		return v
	}
	return 0
}
