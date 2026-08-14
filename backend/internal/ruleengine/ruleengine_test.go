package ruleengine

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/models"
)

func newTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &cache.Cache{RDB: rdb}
}

func mkTx(tenant uuid.UUID) *models.Transaction {
	return &models.Transaction{
		ID:                    uuid.New(),
		TenantID:              tenant,
		Amount:                100,
		Currency:              "USD",
		CustomerIDHash:        "c1",
		CustomerEmailHash:     "e1",
		CardToken:             "card_123",
		IPAddress:             "203.0.113.7",
		DeviceFingerprintHash: "d1",
		CountryCode:           "US",
		TransactionTime:       time.Now().UTC(),
		Metadata:              map[string]any{"base_currency": "USD"},
	}
}

func TestLowRiskTransaction(t *testing.T) {
	c := newTestCache(t)
	cfg := DefaultConfig()
	res, err := Evaluate(context.Background(), mkTx(uuid.New()), cfg, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != models.RiskLow {
		t.Fatalf("expected LOW, got %s (score %0.1f)", res.Level, res.Total)
	}
	if cfg.NeedsAI(res.Total) {
		t.Fatalf("low risk should not trigger AI: %0.1f", res.Total)
	}
}

func TestVelocityHighRisk(t *testing.T) {
	c := newTestCache(t)
	cfg := DefaultConfig()
	tenant := uuid.New()
	base := time.Now()

	// Fire 6 transactions from the same customer within the customer window (limit 5).
	// Cards are unique and devices shared so only the customer velocity fires.
	for i := 0; i < 6; i++ {
		tr := mkTx(tenant)
		tr.CustomerIDHash = "burst-customer"
		tr.CardToken = "card_unique_" + string(rune('a'+i))
		res, err := Evaluate(context.Background(), tr, cfg, c, base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if i < 5 && res.Total > 0 {
			t.Fatalf("expected zero risk below limit, got %0.1f", res.Total)
		}
	}
	lastTx := mkTx(tenant)
	lastTx.CustomerIDHash = "burst-customer"
	last, err := Evaluate(context.Background(), lastTx, cfg, c, base.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if last.Total < 30 {
		t.Fatalf("expected elevated risk after burst, got %0.1f", last.Total)
	}
	if last.Level != models.RiskSuspicious {
		t.Fatalf("expected SUSPICIOUS, got %s", last.Level)
	}
	if cfg.NeedsAI(last.Total) {
		t.Logf("AI triggered at score %0.1f", last.Total)
	}
	found := false
	for _, s := range last.Scores {
		if s.Rule == "velocity_customer" && s.Score > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected velocity_customer rule to fire")
	}
}

func TestGeoBlocklist(t *testing.T) {
	c := newTestCache(t)
	cfg := DefaultConfig()
	cfg.Geo.Blocklist = []string{"XX"}
	cfg.Geo.Weight = 1.0

	tx2 := mkTx(uuid.New())
	tx2.CountryCode = "XX"
	res, err := Evaluate(context.Background(), tx2, cfg, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != models.RiskHigh {
		t.Fatalf("blocklisted country should be HIGH, got %s (score %0.1f)", res.Level, res.Total)
	}
}

func TestIPBlacklist(t *testing.T) {
	c := newTestCache(t)
	cfg := DefaultConfig()
	cfg.IP.Blacklist = []string{"203.0.113.7"}
	cfg.IP.Weight = 1.0

	tx2 := mkTx(uuid.New())
	res, err := Evaluate(context.Background(), tx2, cfg, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Level != models.RiskHigh {
		t.Fatalf("blacklisted IP should be HIGH, got %s (score %0.1f)", res.Level, res.Total)
	}
}

func TestAmountThresholds(t *testing.T) {
	c := newTestCache(t)
	cfg := DefaultConfig()
	cfg.Amount.Weight = 1.0

	big := mkTx(uuid.New())
	big.Amount = 25000 // > max_single
	res, err := Evaluate(context.Background(), big, cfg, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Total < 50 {
		t.Fatalf("huge amount should score high, got %0.1f", res.Total)
	}

	susp := mkTx(uuid.New())
	susp.Amount = 6000 // between suspicious_from and max
	res2, err := Evaluate(context.Background(), susp, cfg, c, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res2.Total >= 30 {
		t.Fatalf("suspicious amount alone should stay below 30, got %0.1f", res2.Total)
	}
}

func TestParseConfigPreservesDefaults(t *testing.T) {
	cfg, err := ParseConfig([]byte(`{"amount":{"max_single":5000}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Amount.MaxSingle != 5000 {
		t.Fatalf("override not applied: %0.1f", cfg.Amount.MaxSingle)
	}
	if cfg.Velocity.Customer.Limit != 5 {
		t.Fatalf("default lost: %0.1f", cfg.Velocity.Customer.Limit)
	}
}

func TestClassifyBands(t *testing.T) {
	cases := []struct {
		score float64
		want  models.RiskLevel
	}{
		{0, models.RiskLow},
		{29.9, models.RiskLow},
		{30, models.RiskSuspicious},
		{69.9, models.RiskSuspicious},
		{70, models.RiskHigh},
		{100, models.RiskHigh},
	}
	for _, tc := range cases {
		cfg := DefaultConfig()
		if got := classify(tc.score, cfg.Decision.AIFloor, cfg.Decision.AICeiling); got != tc.want {
			t.Fatalf("classify(%0.1f) = %s, want %s", tc.score, got, tc.want)
		}
	}
}
