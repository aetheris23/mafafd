package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/security"
	"github.com/mafafd/backend/internal/store"
)

// APICredentials is the outcome of verifying an ingestion API key.
type APICredentials struct {
	Tenant  *models.Tenant
	KeyID   uuid.UUID
	KeyHash string
}

// localTTL is how long verified credentials are trusted in the in-process
// cache. Short enough that revocations propagate quickly, long enough to keep
// the hot path off Redis entirely in steady state.
const localTTL = 10 * time.Second

var (
	localMu sync.Mutex
	local   = make(map[string]localEntry)
)

type localEntry struct {
	creds *APICredentials
	exp   time.Time
}

func localLookup(hash string) *APICredentials {
	localMu.Lock()
	defer localMu.Unlock()
	e, ok := local[hash]
	if !ok {
		return nil
	}
	if time.Now().After(e.exp) {
		delete(local, hash)
		return nil
	}
	return e.creds
}

func localStore(hash string, creds *APICredentials) {
	localMu.Lock()
	defer localMu.Unlock()
	if len(local) > 10000 {
		local = make(map[string]localEntry)
	}
	local[hash] = localEntry{creds: creds, exp: time.Now().Add(localTTL)}
}

// VerifyAPIKey resolves a plaintext API key to a tenant, using an in-process
// hot cache in front of a short-TTL Redis cache to keep the hot path under the
// 20ms SLA (zero Redis round trips for auth on steady state).
func VerifyAPIKey(ctx context.Context, key string, st *store.Store, c *cache.Cache) (*APICredentials, error) {
	hash := security.SHA256Hex(key)

	// 0. In-process hot cache (no Redis / no DB on steady state).
	if creds := localLookup(hash); creds != nil {
		if creds.Tenant.Status != string(models.TenantActive) {
			return nil, store.ErrTenantSuspended
		}
		return creds, nil
	}

	// 1. Redis cache (short-TTL, shared across replicas).
	var cached APICredentials
	if found, err := c.GetJSON(ctx, "auth:key:"+hash, &cached); err == nil && found {
		if cached.Tenant.Status != string(models.TenantActive) {
			return nil, store.ErrTenantSuspended
		}
		creds := &cached
		localStore(hash, creds)
		return creds, nil
	}

	// 2. Cold path: DB lookup.
	tenant, apiKey, err := st.GetTenantByAPIKeyHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	creds := &APICredentials{Tenant: tenant, KeyID: apiKey.ID, KeyHash: hash}

	// 3. Cache for 60s (revocation is honoured at most 60s later; keys are
	//    also evicted on revoke via the gateway).
	_ = c.SetJSON(ctx, "auth:key:"+hash, creds, 60*time.Second)
	localStore(hash, creds)
	go st.TouchAPIKey(ctx, apiKey.ID)
	return creds, nil
}

// VerifyHMACSignature checks the optional X-Mafafd-Signature header.
func VerifyHMACSignature(secret []byte, body []byte, signature string) error {
	if signature == "" {
		return errors.New("missing signature header")
	}
	if !security.VerifyHMAC(secret, body, signature) {
		return errors.New("signature mismatch")
	}
	return nil
}

var _ = fmt.Sprintf // keep fmt imported for future errors
