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

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/ai"
	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/config"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/mq"
	"github.com/mafafd/backend/internal/store"
	"github.com/mafafd/backend/internal/telemetry"
)

type app struct {
	cfg      *config.Config
	log      zerolog.Logger
	metrics  *telemetry.Metrics
	st       *store.Store
	cache    *cache.Cache
	broker   mq.Broker
	provider ai.Provider
}

func main() {
	cfg, err := config.Load("worker-ai")
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

	c, err := cache.New(ctx, cfg.RedisHost, cfg.RedisPort, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, log)
	telemetry.FatalIfErr(err, "connect redis")
	defer c.Close()

	broker, err := mq.New(cfg.MQProvider, cfg.RabbitMQURL, c.RDB, log)
	telemetry.FatalIfErr(err, "connect message broker")
	defer broker.Close()

	provider, err := buildProvider(cfg, log)
	telemetry.FatalIfErr(err, "build AI provider")

	metrics := telemetry.NewMetrics()
	a := &app{cfg: cfg, log: log, metrics: metrics, st: st, cache: c, broker: broker, provider: provider}

	// Expose Prometheus metrics for scraping.
	go telemetry.ServeMetrics(":9092", metrics.Reg, log)

	if err := broker.Consume(ctx, mq.RouteAI, "ai", cfg.WorkerConsumers, a.handleAITask); err != nil {
		log.Fatal().Err(err).Msg("start ai consumer")
	}
	log.Info().Str("provider", provider.Name()).Msg("worker-ai running")
	<-ctx.Done()
	log.Info().Msg("shutting down worker-ai")
}

func buildProvider(cfg *config.Config, log zerolog.Logger) (ai.Provider, error) {
	switch cfg.AIProvider {
	case "gemini":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("AI_PROVIDER=gemini requires GEMINI_API_KEY")
		}
		return ai.NewGeminiProvider(cfg.GeminiAPIKey, cfg.GeminiModel, log), nil
	case "openai":
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("AI_PROVIDER=openai requires OPENAI_API_KEY")
		}
		return ai.NewOpenAIProvider(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL, log), nil
	case "local":
		return ai.NewLocalProvider(), nil
	default:
		return nil, fmt.Errorf("unknown AI_PROVIDER %q", cfg.AIProvider)
	}
}

// handleAITask runs the deep-path AI assessment for suspicious transactions.
func (a *app) handleAITask(ctx context.Context, body []byte) error {
	var task models.AITask
	if err := json.Unmarshal(body, &task); err != nil {
		a.log.Error().Err(err).Msg("malformed ai task")
		return nil // drop permanently
	}
	tx := task.Transaction
	if tx == nil {
		return nil
	}
	a.log.Info().Str("txn", tx.ID.String()).Float64("deterministic", task.DeterministicScore).
		Msg("AI assessment started")

	in := ai.Input{
		Transaction:        tx,
		DeterministicScore: task.DeterministicScore,
		RuleBreakdown:      tx.RuleBreakdown,
		VelocityCounts:     task.VelocityCounts,
	}

	// --- Call the LLM (PII already masked; IP partially masked before egress) ---
	start := time.Now()
	var result *ai.Result
	var providerName = a.provider.Name()
	res, err := a.provider.Analyze(ctx, in)
	if err != nil {
		a.log.Warn().Err(err).Msg("AI provider failed")
		if a.cfg.AIFallbackLocal {
			a.metrics.AIFallbackTotal.Inc()
			a.metrics.AICallsTotal.WithLabelValues(providerName, "fallback").Inc()
			local := ai.NewLocalProvider()
			res, err = local.Analyze(ctx, in)
			providerName = "local(fallback)"
			if err != nil {
				return fmt.Errorf("local fallback failed: %w", err)
			}
		} else {
			a.metrics.AICallsTotal.WithLabelValues(providerName, "error").Inc()
			return err // requeue for retry
		}
	} else {
		a.metrics.AICallsTotal.WithLabelValues(providerName, "success").Inc()
	}
	a.metrics.AILatency.WithLabelValues(providerName).Observe(time.Since(start).Seconds())
	result = res

	// --- Combine deterministic + AI into the final score (escalation-only) ---
	final := task.DeterministicScore
	if result.Score > final {
		final = result.Score
	}
	if final > 100 {
		final = 100
	}
	level := classifyFinal(final)
	verdict := result.Verdict
	if level == models.RiskHigh && !strings.EqualFold(verdict, "HIGH") {
		verdict = string(models.RiskHigh)
	}

	a.log.Info().Str("txn", tx.ID.String()).Str("provider", providerName).
		Float64("ai", result.Score).Float64("final", final).Str("verdict", verdict).Msg("AI assessment complete")

	// --- Persist ---
	if err := a.st.UpdateTransactionAI(ctx, tx.ID, final, level, verdict, result.Explanation); err != nil {
		return fmt.Errorf("update transaction ai: %w", err)
	}
	// Refresh the incident with the AI explanation.
	inc := &models.Incident{
		ID:            uuid.New(),
		TenantID:      tx.TenantID,
		TransactionID: tx.ID,
		RiskScore:     final,
		RiskLevel:     level,
		Reason:        task.Reason,
		AIExplanation: result.Explanation,
		Status:        models.IncidentOpen,
	}
	if err := a.st.InsertIncident(ctx, inc); err != nil {
		a.log.Error().Err(err).Msg("upsert incident with ai")
	}

	// --- Stream the AI verdict to the dashboard ---
	tx.RiskScore = final
	tx.RiskLevel = level
	tx.AIScore = &result.Score
	tx.AIVerdict = verdict
	tx.AIExplanation = result.Explanation
	a.publishEvent(ctx, tx.TenantID.String(), "ai.updated", tx)

	return nil
}

func (a *app) publishEvent(ctx context.Context, tenantID, event string, data any) {
	payload, _ := json.Marshal(models.Event{Event: event, Data: data, At: time.Now().UTC()})
	channel := cache.TenantEventsChannel(tenantID)
	if err := a.cache.Publish(ctx, channel, payload); err != nil {
		a.log.Warn().Err(err).Str("channel", channel).Msg("publish ai event failed")
	}
}

func classifyFinal(score float64) models.RiskLevel {
	switch {
	case score >= 70:
		return models.RiskHigh
	case score >= 30:
		return models.RiskSuspicious
	default:
		return models.RiskLow
	}
}
