package ruleengine

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/store"
)

// LoadConfig returns the effective rule config for a tenant, cached in Redis
// so threshold changes take effect within ~30s without redeploys (dynamic rules).
func LoadConfig(ctx context.Context, tenantID uuid.UUID, st *store.Store, c *cache.Cache) (Config, error) {
	key := cache.TenantRulesKey(tenantID.String())
	var cfg Config
	if found, err := c.GetJSON(ctx, key, &cfg); err == nil && found {
		return cfg, nil
	}
	rc, err := st.GetRuleConfig(ctx, tenantID)
	if errors.Is(err, store.ErrNotFound) {
		cfg = DefaultConfig()
		_ = c.SetJSON(ctx, key, cfg, 30*time.Second)
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err = ParseConfig(rc.Data)
	if err != nil {
		return Config{}, err
	}
	_ = c.SetJSON(ctx, key, cfg, 30*time.Second)
	return cfg, nil
}
