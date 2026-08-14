package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/auth"
	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/config"
	"github.com/mafafd/backend/internal/httpserver"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/mq"
	"github.com/mafafd/backend/internal/security"
	"github.com/mafafd/backend/internal/store"
	"github.com/mafafd/backend/internal/telemetry"
)

type app struct {
	cfg    *config.Config
	log    zerolog.Logger
	metrics *telemetry.Metrics
	st     *store.Store
	cache  *cache.Cache
	broker mq.Broker
}

func main() {
	cfg, err := config.Load("ingestion")
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	log := telemetry.InitLogger(cfg.Service, cfg.LogLevel)
	log.Info().Str("env", cfg.Env).Msg("starting ingestion service")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL, log)
	telemetry.FatalIfErr(err, "connect postgres")
	defer st.Close()

	c, err := cache.New(ctx, cfg.RedisHost, cfg.RedisPort, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, log)
	telemetry.FatalIfErr(err, "connect redis")
	defer c.Close()

	broker, err := mq.New(cfg.MQProvider, cfg.RabbitMQURL, c.RDB, log)
	telemetry.FatalIfErr(err, "connect message broker")
	defer broker.Close()

	metrics := telemetry.NewMetrics()
	a := &app{cfg: cfg, log: log, metrics: metrics, st: st, cache: c, broker: broker}

	f := httpserver.NewApp(log, metrics)
	f.Use(httpserver.MetricsMiddleware(metrics))

	f.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "ingestion", "time": time.Now()})
	})
	f.Get("/ready", a.ready)
	f.Get("/metrics", adaptor.HTTPHandler(metrics.HTTPHandler()))

	api := f.Group("/api/v1")
	api.Post("/transactions", a.handleTransaction)

	go func() {
		if err := f.Listen(fmt.Sprintf(":%d", cfg.Port)); err != nil {
			log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	log.Info().Int("port", cfg.Port).Msg("ingestion listening")
	<-ctx.Done()
	log.Info().Msg("shutting down ingestion")
	_ = f.ShutdownWithTimeout(5 * time.Second)
}

func (a *app) ready(c *fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{}
	if err := a.st.Ping(ctx); err != nil {
		checks["postgres"] = "down"
	} else {
		checks["postgres"] = "ok"
	}
	if err := a.cache.RDB.Ping(ctx).Err(); err != nil {
		checks["redis"] = "down"
	} else {
		checks["redis"] = "ok"
	}
	if err := a.broker.Healthy(ctx); err != nil {
		checks["mq"] = "down"
	} else {
		checks["mq"] = "ok"
	}
	for _, v := range checks {
		if v != "ok" {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "not_ready", "checks": checks})
		}
	}
	return c.JSON(fiber.Map{"status": "ready", "checks": checks})
}

type txnResponse struct {
	TransactionID   string    `json:"transaction_id"`
	Status          string    `json:"status"`
	Processing      string    `json:"processing"`
	ReceivedAt      time.Time `json:"received_at"`
	RiskAssessment  string    `json:"risk_assessment"`
	EstMs           float64   `json:"estimated_processing_ms"`
	RequestID       string    `json:"request_id"`
}

// handleTransaction is the hot path: auth -> rate limit -> validate -> mask -> publish.
func (a *app) handleTransaction(c *fiber.Ctx) error {
	start := time.Now()
	ctx := c.Context()

	// --- 1. Tenant authentication (API key) ---
	key := c.Get("X-Api-Key")
	if key == "" {
		key = strings.TrimPrefix(c.Get(fiber.HeaderAuthorization), "Bearer ")
	}
	if key == "" {
		a.metrics.TxRejectedTotal.WithLabelValues("missing_api_key").Inc()
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing API key"})
	}
	creds, err := auth.VerifyAPIKey(ctx, key, a.st, a.cache)
	if err != nil {
		a.log.Warn().Str("ip", c.IP()).Msg("api key rejected")
		a.metrics.TxRejectedTotal.WithLabelValues("invalid_api_key").Inc()
		if err == store.ErrTenantSuspended {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "tenant suspended"})
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid API key"})
	}
	c.Locals("tenant_id", creds.Tenant.ID)

	// --- 2. Per-tenant rate limiting (token bucket / noisy-neighbour isolation) ---
	rate := a.cfg.TokenRatePerMin
	burst := a.cfg.TokenBurst
	if st := tenantSettings(creds.Tenant); st != nil {
		if r, ok := st["rate_limit_per_min"].(float64); ok && r > 0 {
			rate = r
		}
		if b, ok := st["burst"].(float64); ok && b > 0 {
			burst = b
		}
	}
	rl, err := a.cache.TokenBucketConsume(ctx, "ratelimit:"+creds.Tenant.ID.String(), rate, burst, time.Now())
	if err != nil {
		a.log.Warn().Err(err).Msg("rate limiter error")
	} else if !rl.Allowed {
		a.metrics.TxRejectedTotal.WithLabelValues("rate_limited").Inc()
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", burst))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", rl.Remaining))
		c.Set("Retry-After", fmt.Sprintf("%d", rl.RetryAfterMs/1000))
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "rate limit exceeded", "retry_after_seconds": rl.RetryAfterMs / 1000,
		})
	}
	c.Set("X-RateLimit-Limit", fmt.Sprintf("%.0f", burst))
	c.Set("X-RateLimit-Remaining", fmt.Sprintf("%.0f", rl.Remaining))

	// --- 3. Parse & validate (strict schema) ---
	var in models.TransactionInput
	body := c.Body()
	if err := json.Unmarshal(body, &in); err != nil {
		a.metrics.TxRejectedTotal.WithLabelValues("invalid_json").Inc()
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON payload"})
	}
	if verr := validateInput(&in); verr != "" {
		a.metrics.TxRejectedTotal.WithLabelValues("validation").Inc()
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": verr})
	}

	// --- 4. Optional request signing (HMAC) for defence in depth ---
	if st := tenantSettings(creds.Tenant); st != nil {
		if secret, ok := st["signing_secret"].(string); ok && secret != "" {
			sig := c.Get("X-Mafafd-Signature")
			if !security.VerifyHMAC([]byte(secret), body, sig) {
				a.metrics.TxRejectedTotal.WithLabelValues("bad_signature").Inc()
				a.st.InsertAudit(ctx, &creds.Tenant.ID, nil, "signature_mismatch", "transaction", "", nil)
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid signature"})
			}
		}
	}

	// --- 5. Mask PII at the edge and build the canonical record ---
	ts := time.Now().UTC()
	if in.Timestamp != nil {
		ts = in.Timestamp.UTC()
	}
	tx := &models.Transaction{
		ID:                  uuid.New(),
		TenantID:            creds.Tenant.ID,
		ExternalID:          in.ExternalID,
		Amount:              in.Amount,
		Currency:            strings.ToUpper(in.Currency),
		CustomerIDHash:      security.MaskID(in.CustomerID),
		CustomerEmailHash:   security.MaskEmail(in.CustomerEmail),
		CardToken:           security.MaskCard(in.CardToken),
		IPAddress:           in.IPAddress,
		DeviceFingerprintHash: security.MaskID(in.DeviceFingerprint),
		CountryCode:         strings.ToUpper(in.CountryCode),
		City:                in.City,
		MerchantCategory:    in.MerchantCategory,
		Metadata:            in.Metadata,
		Status:              models.TxnProcessed,
		TransactionTime:     ts,
		CreatedAt:           time.Now().UTC(),
		RuleBreakdown:       []models.RuleScore{},
	}
	if tx.Metadata == nil {
		tx.Metadata = map[string]any{}
	}
	tx.Metadata["base_currency"] = creds.Tenant.BaseCurrency
	tx.Metadata["request_id"] = reqID(c)

	// --- 6. Non-blocking publish ---
	payload, err := json.Marshal(tx)
	if err != nil {
		a.metrics.TxRejectedTotal.WithLabelValues("marshal").Inc()
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "encode failed"})
	}
	if err := a.broker.Publish(ctx, mq.RouteTransactionNew, payload); err != nil {
		a.log.Error().Err(err).Str("tenant", creds.Tenant.ID.String()).Msg("publish failed")
		a.metrics.TxRejectedTotal.WithLabelValues("mq_unavailable").Inc()
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "temporarily unavailable"})
	}
	a.metrics.TxIngestedTotal.Inc()
	a.metrics.TxPublishedTotal.Inc()

	elapsed := time.Since(start)
	a.log.Debug().
		Str("request_id", reqID(c)).Str("tenant", creds.Tenant.ID.String()).
		Str("txn_id", tx.ID.String()).Float64("amount", tx.Amount).
		Dur("latency", elapsed).Msg("transaction accepted")

	return c.Status(fiber.StatusAccepted).JSON(txnResponse{
		TransactionID:  tx.ID.String(),
		Status:         "accepted",
		Processing:     "async",
		ReceivedAt:     tx.CreatedAt,
		RiskAssessment: "queued",
		EstMs:          elapsed.Seconds() * 1000,
		RequestID:      reqID(c),
	})
}

func validateInput(in *models.TransactionInput) string {
	if in.Amount <= 0 {
		return "amount must be positive"
	}
	if len(in.Currency) != 3 {
		return "currency must be a 3-letter ISO code"
	}
	if in.CustomerID == "" && in.CustomerEmail == "" && in.CardToken == "" {
		return "at least one of customer_id, customer_email, card_token is required"
	}
	if in.CustomerEmail != "" && !strings.Contains(in.CustomerEmail, "@") {
		return "customer_email is invalid"
	}
	if len(in.ExternalID) > 128 {
		return "external_id too long"
	}
	if len(in.City) > 128 {
		return "city too long"
	}
	if len(in.CountryCode) > 3 {
		return "country_code invalid"
	}
	return ""
}

func tenantSettings(t *models.Tenant) map[string]any {
	if len(t.Settings) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(t.Settings, &m); err != nil {
		return nil
	}
	return m
}

func reqID(c *fiber.Ctx) string {
	if v, ok := c.Locals("request_id").(string); ok {
		return v
	}
	return ""
}
