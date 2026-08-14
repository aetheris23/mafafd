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

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/config"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/mq"
	"github.com/mafafd/backend/internal/ruleengine"
	"github.com/mafafd/backend/internal/store"
	"github.com/mafafd/backend/internal/telemetry"
)

type app struct {
	cfg     *config.Config
	log     zerolog.Logger
	metrics *telemetry.Metrics
	st      *store.Store
	cache   *cache.Cache
	broker  mq.Broker
}

func main() {
	cfg, err := config.Load("worker-rules")
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

	metrics := telemetry.NewMetrics()
	a := &app{cfg: cfg, log: log, metrics: metrics, st: st, cache: c, broker: broker}

	// Expose Prometheus metrics for scraping.
	go telemetry.ServeMetrics(":9091", metrics.Reg, log)

	// Report queue depth for autoscaling signals.
	go a.monitorQueues(ctx)

	if err := broker.Consume(ctx, mq.RouteTransactionNew, "rules", cfg.WorkerConsumers, a.handleTransaction); err != nil {
		log.Fatal().Err(err).Msg("start rules consumer")
	}
	log.Info().Msg("worker-rules running")
	<-ctx.Done()
	log.Info().Msg("shutting down worker-rules")
}

func (a *app) handleTransaction(ctx context.Context, body []byte) error {
	start := time.Now()
	a.metrics.TxConsumedTotal.Inc()

	var tx models.Transaction
	if err := json.Unmarshal(body, &tx); err != nil {
		a.log.Error().Err(err).Msg("malformed message")
		return nil // permanent failure, drop
	}
	a.log.Debug().Str("txn", tx.ID.String()).Str("tenant", tx.TenantID.String()).Msg("evaluating")

	cfg, err := ruleengine.LoadConfig(ctx, tx.TenantID, a.st, a.cache)
	if err != nil {
		return fmt.Errorf("load rules: %w", err)
	}

	res, err := ruleengine.Evaluate(ctx, &tx, cfg, a.cache, time.Now())
	if err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}

	tx.DeterministicScore = res.Total
	tx.RiskScore = res.Total
	tx.RiskLevel = res.Level
	tx.RuleBreakdown = res.Scores
	now := time.Now()
	tx.ProcessedAt = &now

	// Persist the canonical record first.
	if err := a.st.InsertTransaction(ctx, &tx); err != nil {
		return fmt.Errorf("persist: %w", err)
	}

	a.metrics.TxDecisionTotal.WithLabelValues(string(res.Level)).Inc()
	a.metrics.RuleLatency.WithLabelValues("total").Observe(time.Since(start).Seconds())

	// Publish to the dashboard event stream (all transactions, for live charts).
	a.publishEvent(ctx, tx.TenantID.String(), "transaction.processed", tx)

	// Route to the async AI pipeline when score is in the band 30-70,
	// or when already high (for explanation).
	if cfg.NeedsAI(res.Total) || res.Level == models.RiskHigh {
		task := models.AITask{
			Transaction:        &tx,
			VelocityCounts:     res.VelocityCounts,
			DeterministicScore: res.Total,
			Reason:             strings.Join(res.Findings, "; "),
		}
		payload, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal ai task: %w", err)
		}
		if err := a.broker.Publish(ctx, mq.RouteAI, payload); err != nil {
			a.log.Error().Err(err).Str("txn", tx.ID.String()).Msg("publish to ai failed")
			return err
		}
	}

	// Create an incident when the risk is material.
	if cfg.CreatesIncident(res.Total) {
		inc := &models.Incident{
			ID:            uuid.New(),
			TenantID:      tx.TenantID,
			TransactionID: tx.ID,
			RiskScore:     res.Total,
			RiskLevel:     res.Level,
			Reason:        strings.Join(res.Findings, "; "),
			Status:        models.IncidentOpen,
		}
		if err := a.st.InsertIncident(ctx, inc); err != nil {
			a.log.Error().Err(err).Msg("insert incident")
			return err
		}
		a.log.Info().Str("txn", tx.ID.String()).Float64("score", res.Total).Msg("anomaly incident flagged")
		a.publishEvent(ctx, tx.TenantID.String(), "incident.flagged", inc)
	}

	return nil
}

func (a *app) publishEvent(ctx context.Context, tenantID, event string, data any) {
	payload, _ := json.Marshal(models.Event{
		Event: event,
		Data:  data,
		At:    time.Now().UTC(),
	})
	channel := cache.TenantEventsChannel(tenantID)
	if err := a.cache.Publish(ctx, channel, payload); err != nil {
		a.log.Warn().Err(err).Str("channel", channel).Msg("publish event failed")
	}
}

func (a *app) monitorQueues(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			depth, err := a.broker.Depth(ctx)
			if err != nil {
				continue
			}
			for q, n := range depth {
				a.metrics.QueueDepth.WithLabelValues(q).Set(float64(n))
			}
		}
	}
}
