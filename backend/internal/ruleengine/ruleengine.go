package ruleengine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/security"
)

// Config is the dynamic, per-tenant deterministic rule configuration.
type Config struct {
	Velocity struct {
		Customer        VelocityRule `json:"customer"`
		IP              VelocityRule `json:"ip"`
		Card            VelocityRule `json:"card"`
		Device          VelocityRule `json:"device"`
		GlobalPerMinute struct {
			Limit  float64 `json:"limit"`
			Weight float64 `json:"weight"`
		} `json:"global_per_minute"`
	} `json:"velocity"`
	Geo struct {
		Allowlist       []string `json:"allowlist"`
		Blocklist       []string `json:"blocklist"`
		BlocklistScore  float64  `json:"blocklist_score"`
		UnknownScore    float64  `json:"unknown_score"`
		Weight          float64  `json:"weight"`
	} `json:"geo"`
	IP struct {
		Blacklist []string `json:"blacklist"`
		Score     float64  `json:"score"`
		Weight    float64  `json:"weight"`
	} `json:"ip"`
	Amount struct {
		MaxSingle        float64 `json:"max_single"`
		SuspiciousFrom   float64 `json:"suspicious_from"`
		Weight           float64 `json:"weight"`
	} `json:"amount"`
	Currency struct {
		Weight float64 `json:"weight"`
	} `json:"currency"`
	Decision struct {
		AIFloor           float64 `json:"ai_floor"`           // >= this -> go to AI
		AICeiling         float64 `json:"ai_ceiling"`         // < this -> only AI escalation possible
		IncidentThreshold float64 `json:"incident_threshold"` // >= this -> incident
	} `json:"decision"`
}

type VelocityRule struct {
	WindowSec float64 `json:"window_sec"`
	Limit     float64 `json:"limit"`
	Weight    float64 `json:"weight"`
}

// DefaultConfig is applied when a tenant has no custom rules.
func DefaultConfig() Config {
	c := Config{}
	j := json.RawMessage(`{
	  "velocity": {
	    "customer":  {"window_sec": 600, "limit": 5,   "weight": 0.25},
	    "ip":        {"window_sec": 60,  "limit": 12,  "weight": 0.20},
	    "card":      {"window_sec": 600, "limit": 3,   "weight": 0.20},
	    "device":    {"window_sec": 600, "limit": 3,   "weight": 0.10},
	    "global_per_minute": {"limit": 100, "weight": 0.15}
	  },
	  "geo":        {"allowlist": ["US","ID","SG"], "blocklist": [], "blocklist_score": 70, "unknown_score": 15, "weight": 0.15},
	  "ip":         {"blacklist": [], "score": 75, "weight": 0.20},
	  "amount":     {"max_single": 10000, "suspicious_from": 5000, "weight": 0.20},
	  "currency":   {"weight": 0.05},
	  "decision":   {"ai_floor": 30, "ai_ceiling": 70, "incident_threshold": 40}
	}`)
	_ = json.Unmarshal(j, &c)
	return c
}

// ParseConfig unmarshals raw JSON (with defaults for anything missing).
func ParseConfig(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("parse rule config: %w", err)
		}
	}
	return cfg, nil
}

// Result is the outcome of the deterministic engine.
type Result struct {
	Scores         []models.RuleScore
	Total          float64
	Level          models.RiskLevel
	Findings       []string
	VelocityCounts map[string]int64
}

type evaluator struct {
	cfg    Config
	c      *cache.Cache
	txn    *models.Transaction
	now    time.Time
	keys   map[string]string
	counts map[string]int64
}

// Evaluate runs the hybrid fast-path rules for a transaction.
func Evaluate(ctx context.Context, txn *models.Transaction, cfg Config, c *cache.Cache, now time.Time) (*Result, error) {
	e := &evaluator{
		cfg:    cfg,
		c:      c,
		txn:    txn,
		now:    now,
		keys:   map[string]string{},
		counts: map[string]int64{},
	}
	scores := make([]models.RuleScore, 0, 8)
	weigthsTotal := 0.0
	weighted := 0.0
	findings := []string{}

	add := func(rule string, score, weight float64, detail string) {
		if score <= 0 || weight <= 0 {
			return
		}
		scores = append(scores, models.RuleScore{Rule: rule, Score: score, Weight: weight, Detail: detail})
		weighted += score * weight
		weigthsTotal += weight
	}

	run := func(name string, fn func(context.Context) (float64, string, error)) error {
		start := time.Now()
		score, detail, err := fn(ctx)
		if err != nil {
			return err
		}
		if score > 0 {
			findings = append(findings, detail)
		}
		// latency is captured by the caller's metrics wrapper
		_ = start
		return nil
	}

	_ = run("velocity", func(ctx context.Context) (float64, string, error) {
		var score float64
		if txn.CustomerIDHash != "" {
			cnt, err := e.count(ctx, "customer", txn.CustomerIDHash, e.cfg.Velocity.Customer)
			if err != nil {
				return 0, "", err
			}
			add("velocity_customer", velocityScore(float64(cnt), e.cfg.Velocity.Customer.Limit, 2.0), e.cfg.Velocity.Customer.Weight,
				fmt.Sprintf("%d txns for customer in %ds window (limit %d)", cnt, int(e.cfg.Velocity.Customer.WindowSec), int(e.cfg.Velocity.Customer.Limit)))
			score = velocityScore(float64(cnt), e.cfg.Velocity.Customer.Limit, 2.0)
		}
		if txn.IPAddress != "" {
			cnt, err := e.count(ctx, "ip", txn.IPAddress, e.cfg.Velocity.IP)
			if err != nil {
				return 0, "", err
			}
			add("velocity_ip", velocityScore(float64(cnt), e.cfg.Velocity.IP.Limit, 2.0), e.cfg.Velocity.IP.Weight,
				fmt.Sprintf("%d txns from IP %s in %ds", cnt, security.MaskIP(txn.IPAddress), int(e.cfg.Velocity.IP.WindowSec)))
		}
		if txn.CardToken != "" {
			cnt, err := e.count(ctx, "card", txn.CardToken, e.cfg.Velocity.Card)
			if err != nil {
				return 0, "", err
			}
			add("velocity_card", velocityScore(float64(cnt), e.cfg.Velocity.Card.Limit, 2.0), e.cfg.Velocity.Card.Weight,
				fmt.Sprintf("%d txns for card in %ds", cnt, int(e.cfg.Velocity.Card.WindowSec)))
		}
		if txn.DeviceFingerprintHash != "" && txn.CustomerIDHash != "" {
			cnt, err := e.distinctDevices(ctx, txn.CustomerIDHash, txn.DeviceFingerprintHash, e.cfg.Velocity.Device)
			if err != nil {
				return 0, "", err
			}
			add("velocity_device", velocityScore(float64(cnt), e.cfg.Velocity.Device.Limit, 1.5), e.cfg.Velocity.Device.Weight,
				fmt.Sprintf("%d distinct devices for customer in %ds", cnt, int(e.cfg.Velocity.Device.WindowSec)))
		}
		global, err := e.count(ctx, "global", "all", VelocityRule{
			WindowSec: 60,
			Limit:     e.cfg.Velocity.GlobalPerMinute.Limit,
			Weight:    e.cfg.Velocity.GlobalPerMinute.Weight,
		})
		if err != nil {
			return 0, "", err
		}
		add("velocity_global", velocityScore(float64(global), e.cfg.Velocity.GlobalPerMinute.Limit, 2.0), e.cfg.Velocity.GlobalPerMinute.Weight,
			fmt.Sprintf("%d tenant-wide txns this minute (limit %d)", global, int(e.cfg.Velocity.GlobalPerMinute.Limit)))
		return score, "", nil
	})

	_ = run("geo", func(ctx context.Context) (float64, string, error) {
		cc := strings.ToUpper(txn.CountryCode)
		for _, b := range e.cfg.Geo.Blocklist {
			if strings.EqualFold(b, cc) {
				add("geo_blocklist", e.cfg.Geo.BlocklistScore, e.cfg.Geo.Weight, "country "+cc+" is blocklisted")
				return e.cfg.Geo.BlocklistScore, "blocklisted country", nil
			}
		}
		if cc != "" {
			allowed := len(e.cfg.Geo.Allowlist) == 0
			for _, a := range e.cfg.Geo.Allowlist {
				if strings.EqualFold(a, cc) {
					allowed = true
					break
				}
			}
			if !allowed {
				add("geo_unknown", e.cfg.Geo.UnknownScore, e.cfg.Geo.Weight, "country "+cc+" not in allowlist")
				return e.cfg.Geo.UnknownScore, "unexpected country", nil
			}
		}
		return 0, "", nil
	})

	_ = run("ip_blacklist", func(ctx context.Context) (float64, string, error) {
		if txn.IPAddress == "" {
			return 0, "", nil
		}
		for _, b := range e.cfg.IP.Blacklist {
			if b == txn.IPAddress {
				add("ip_blacklist", e.cfg.IP.Score, e.cfg.IP.Weight, "IP "+txn.IPAddress+" is blacklisted")
				return e.cfg.IP.Score, "blacklisted IP", nil
			}
		}
		return 0, "", nil
	})

	_ = run("amount", func(ctx context.Context) (float64, string, error) {
		if txn.Amount >= e.cfg.Amount.MaxSingle {
			s := 60.0
			add("amount_max", s, e.cfg.Amount.Weight, fmt.Sprintf("amount %.2f exceeds max single %.2f", txn.Amount, e.cfg.Amount.MaxSingle))
			return s, "excessive amount", nil
		}
		if txn.Amount >= e.cfg.Amount.SuspiciousFrom {
			s := 25.0
			add("amount_suspicious", s, e.cfg.Amount.Weight, fmt.Sprintf("amount %.2f above suspicious threshold %.2f", txn.Amount, e.cfg.Amount.SuspiciousFrom))
			return s, "suspicious amount", nil
		}
		return 0, "", nil
	})

	_ = run("currency", func(ctx context.Context) (float64, string, error) {
		if txn.Currency != "" {
			// currency mismatch vs. tenant base currency is surfaced in the worker;
			// here we accept a base hint via metadata to keep engine decoupled.
			if base, ok := txn.Metadata["base_currency"].(string); ok && base != "" && !strings.EqualFold(base, txn.Currency) {
				s := 15.0
				add("currency_mismatch", s, e.cfg.Currency.Weight, "currency "+txn.Currency+" differs from base "+base)
				return s, "currency mismatch", nil
			}
		}
		return 0, "", nil
	})

	total := 0.0
	if weigthsTotal > 0 {
		total = weighted / weigthsTotal
	}
	level := classify(total, e.cfg.Decision.AIFloor, e.cfg.Decision.AICeiling)
	return &Result{Scores: scores, Total: total, Level: level, Findings: findings, VelocityCounts: e.counts}, nil
}

func velocityScore(count, limit, steepness float64) float64 {
	if limit <= 0 {
		return 0
	}
	if count <= limit {
		return 0
	}
	ratio := (count - limit) / limit
	s := 30.0 + ratio*steepness*20.0
	if s > 100 {
		s = 100
	}
	return s
}

func (e *evaluator) count(ctx context.Context, scope, id string, rule VelocityRule) (int64, error) {
	key := fmt.Sprintf("vel:%s:%s:%s", e.txn.TenantID, scope, id)
	window := time.Duration(rule.WindowSec) * time.Second
	n, err := e.c.SlidingWindowAdd(ctx, key, e.txn.ID.String(), e.now, window)
	if err == nil {
		e.counts[scope] = n
	}
	return n, err
}

func (e *evaluator) distinctDevices(ctx context.Context, customerID, deviceHash string, rule VelocityRule) (int64, error) {
	key := fmt.Sprintf("vel:dev:%s:%s", e.txn.TenantID, customerID)
	window := time.Duration(rule.WindowSec) * time.Second
	// member = device hash -> ZCARD counts distinct devices in window
	n, err := e.c.SlidingWindowAdd(ctx, key, deviceHash, e.now, window)
	if err == nil {
		e.counts["distinct_devices"] = n
	}
	return n, err
}

func classify(score, floor, ceiling float64) models.RiskLevel {
	switch {
	case score >= ceiling:
		return models.RiskHigh
	case score >= floor:
		return models.RiskSuspicious
	default:
		return models.RiskLow
	}
}

// NeedsAI returns true when the deterministic score falls in the AI band.
func (cfg Config) NeedsAI(score float64) bool {
	return score >= cfg.Decision.AIFloor && score < cfg.Decision.AICeiling
}

// CreatesIncident reports whether this score warrants an anomaly incident.
func (cfg Config) CreatesIncident(score float64) bool {
	return score >= cfg.Decision.IncidentThreshold
}
