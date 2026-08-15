package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/ai"
	"github.com/mafafd/backend/internal/data"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/ruleengine"
	"github.com/mafafd/backend/internal/security"
	"github.com/mafafd/backend/internal/store"
)

const (
	PlatformEmail  = "platform@mafafd.dev"
	PlatformPass   = "Mafafd-Platform-2026!"
	DemoTenantName = "Acme Payments"
	DemoAdminEmail = "admin@acme.dev"
	DemoAdminPass  = "Acme-Admin-2026!"
)

// DemoAPIKey is the pre-issued merchant key (dev only). Regenerate in prod.
const DemoAPIKey = "sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d"

// Run seeds the platform on first boot when no tenants exist.
func Run(ctx context.Context, st *store.Store, log zerolog.Logger) error {
	tenants, err := st.ListTenants(ctx)
	if err != nil {
		return err
	}
	if len(tenants) > 0 {
		log.Info().Msg("seed skipped: tenants already present")
		return nil
	}

	tenantID := uuid.New()

	// --- platform user ---
	platformHash, _ := security.HashPassword(PlatformPass)
	if _, err := st.CreateUser(ctx, uuid.Nil, PlatformEmail, platformHash, models.RolePlatform); err != nil {
		return fmt.Errorf("seed platform user: %w", err)
	}

	// --- tenant ---
	settings, _ := json.Marshal(map[string]any{
		"rate_limit_per_min": 600.0,
		"burst":              1200.0,
		"signing_secret":     "",
	})
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO tenants (id, name, status, base_currency, settings) VALUES ($1,$2,'active','USD',$3)`,
		tenantID, DemoTenantName, settings); err != nil {
		return fmt.Errorf("seed tenant: %w", err)
	}

	// --- admin user ---
	adminHash, _ := security.HashPassword(DemoAdminPass)
	if _, err := st.CreateUser(ctx, tenantID, DemoAdminEmail, adminHash, models.RoleAdmin); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	// --- merchant API key ---
	hash := security.SHA256Hex(DemoAPIKey)
	if _, err := st.CreateAPIKey(ctx, tenantID, "default production key", hash, DemoAPIKey[:12]); err != nil {
		return fmt.Errorf("seed api key: %w", err)
	}

	// --- default rule config ---
	def := ruleengine.DefaultConfig()
	raw, _ := json.Marshal(def)
	if err := st.UpsertRuleConfig(ctx, tenantID, raw, uuid.Nil); err != nil {
		return fmt.Errorf("seed rules: %w", err)
	}

	// --- historical transaction volume for charts ---
	if err := seedHistoricalTransactions(ctx, st, tenantID, log); err != nil {
		return err
	}

	log.Info().Str("tenant", DemoTenantName).Str("tenant_id", tenantID.String()).Msg("demo data seeded")
	return nil
}

func seedHistoricalTransactions(ctx context.Context, st *store.Store, tenantID uuid.UUID, log zerolog.Logger) error {
	rng := rand.New(rand.NewSource(42))
	now := time.Now().UTC().Truncate(time.Hour)
	currencies := []string{"USD", "EUR", "IDR", "SGD"}
	countries := map[string]string{"US": "New York", "ID": "Jakarta", "SG": "Singapore", "MY": "Kuala Lumpur", "EU": "Berlin"}

	// customer pool (hashed ids) and devices
	type profile struct {
		cust  string
		email string
		card  string
		dev   string
		ip    string
		cc    string
		city  string
	}
	var profiles []profile
	for i := 0; i < 24; i++ {
		cc := []string{"US", "ID", "SG", "MY", "EU"}[rng.Intn(5)]
		p := profile{
			cust:  security.MaskID(fmt.Sprintf("cust-%d", i)),
			email: security.MaskEmail(fmt.Sprintf("cust%d@example.com", i)),
			card:  security.MaskCard(fmt.Sprintf("4111-1111-1111-%04d", 1000+i)),
			dev:   security.MaskID(fmt.Sprintf("dev-%d", i)),
			ip:    fmt.Sprintf("203.0.%d.%d", rng.Intn(2)+1, rng.Intn(254)+1),
			cc:    cc,
			city:  countries[cc],
		}
		profiles = append(profiles, p)
	}

	// suspicious profiles
	risky := []int{2, 5, 9, 17}

	batch := &store.TransactionBatch{}
	total := 0

	for h := 0; h < 24*7; h++ {
		ts := now.Add(-time.Duration(h) * time.Hour)
		hour := ts.Hour()
		// volume varies by time of day (0..3)
		base := 4 + (9-hour%12)/4
		n := base + rng.Intn(6)
		if hour >= 22 || hour <= 4 {
			n = 1 + rng.Intn(3)
		}
		for i := 0; i < n; i++ {
			p := profiles[rng.Intn(len(profiles))]
			riskyNow := contains(risky, rng.Intn(24))
			amount := 5.0 + float64(rng.Intn(500))
			if riskyNow && rng.Float64() < 0.4 {
				amount = 5000 + float64(rng.Intn(15000))
			}
			cur := currencies[rng.Intn(len(currencies))]
			merchant := data.RandomMerchant(rng, p.cc)

			// deterministic-ish score preview
			score := 8 + rng.Float64()*12
			if riskyNow {
				score = 45 + rng.Float64()*50
			}
			level := classifySeed(score)
			isAnomaly := score >= 40
			txID := uuid.New()
			breakdown := []models.RuleScore{}
			if isAnomaly {
				breakdown = seededBreakdown(rng, amount, cur, p.cc)
			}
			tx := models.Transaction{
				ID:                    txID,
				TenantID:              tenantID,
				ExternalID:            fmt.Sprintf("EXT-%d", rng.Intn(1_000_000)),
				Amount:                amount,
				Currency:              cur,
				CustomerIDHash:        p.cust,
				CustomerEmailHash:     p.email,
				CardToken:             p.card,
				IPAddress:             p.ip,
				DeviceFingerprintHash: p.dev,
				CountryCode:           p.cc,
				City:                  p.city,
				MerchantCategory:      merchant.Category,
				MerchantName:          merchant.Name,
				PaymentMethod:         data.RandomPaymentMethod(rng, p.cc),
				RiskScore:             score,
				RiskLevel:             level,
				DeterministicScore:    score,
				Status:                models.TxnProcessed,
				TransactionTime:       ts,
				CreatedAt:             ts,
				IsAnomaly:             isAnomaly,
				RuleBreakdown:         breakdown,
				Metadata:              map[string]any{"base_currency": "USD", "seeded": true},
			}
			if isAnomaly {
				tx.Status = models.TxnFlagged
				expl := ai.LocalExplain(&tx, tx.RuleBreakdown, nil, score)
				scorePtr := score
				tx.AIScore = &scorePtr
				tx.AIVerdict = string(level)
				tx.AIExplanation = expl
				batch.Incidents = append(batch.Incidents, models.Incident{
					ID:            uuid.New(),
					TenantID:      tenantID,
					TransactionID: txID,
					RiskScore:     score,
					RiskLevel:     level,
					Reason:        "seeded anomaly pattern",
					AIExplanation: expl,
					Status:        models.IncidentOpen,
					CreatedAt:     ts,
					UpdatedAt:     ts,
				})
			}
			batch.Transactions = append(batch.Transactions, tx)
			total++
		}
		if len(batch.Transactions) >= 500 {
			if err := st.BatchInsert(ctx, batch); err != nil {
				return err
			}
			batch = &store.TransactionBatch{}
		}
	}
	if len(batch.Transactions) > 0 {
		if err := st.BatchInsert(ctx, batch); err != nil {
			return err
		}
	}
	log.Info().Int("transactions", total).Msg("historical transactions seeded")
	return nil
}

func classifySeed(score float64) models.RiskLevel {
	switch {
	case score >= 70:
		return models.RiskHigh
	case score >= 30:
		return models.RiskSuspicious
	default:
		return models.RiskLow
	}
}

// seededBreakdown builds a plausible deterministic rule breakdown for seeded
// anomalies so the AI explanation reads logically instead of generically.
func seededBreakdown(rng *rand.Rand, amount float64, currency, country string) []models.RuleScore {
	maxSingle, suspicious := 10000.00, 3000.00
	switch currency {
	case "IDR":
		maxSingle, suspicious = 15_000_000.00, 3_000_000.00
	case "JPY":
		maxSingle, suspicious = 1_000_000.00, 300_000.00
	}
	out := []models.RuleScore{
		{
			Rule:   "velocity_customer",
			Score:  40 + rng.Float64()*20,
			Weight: 0.25,
			Detail: fmt.Sprintf("%d transactions for customer in 600s window (limit 5)", 6+rng.Intn(4)),
		},
	}
	switch {
	case amount >= maxSingle:
		out = append(out, models.RuleScore{
			Rule:   "amount_max",
			Score:  60,
			Weight: 0.20,
			Detail: fmt.Sprintf("amount %.2f exceeds max single %.2f", amount, maxSingle),
		})
	case amount >= suspicious:
		out = append(out, models.RuleScore{
			Rule:   "amount_suspicious",
			Score:  30,
			Weight: 0.20,
			Detail: fmt.Sprintf("amount %.2f above suspicious threshold %.2f", amount, suspicious),
		})
	}
	if country != "" && country != "ID" && country != "US" {
		out = append(out, models.RuleScore{
			Rule:   "geo_unknown",
			Score:  30 + rng.Float64()*20,
			Weight: 0.15,
			Detail: fmt.Sprintf("country %s not in allowlist", country),
		})
	}
	if rng.Float64() < 0.3 {
		out = append(out, models.RuleScore{
			Rule:   "velocity_card",
			Score:  25 + rng.Float64()*15,
			Weight: 0.20,
			Detail: fmt.Sprintf("%d transactions for card in 600s", 3+rng.Intn(3)),
		})
	}
	return out
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var _ = strings.ToUpper
