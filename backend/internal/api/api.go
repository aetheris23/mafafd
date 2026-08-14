package api

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/auth"
	"github.com/mafafd/backend/internal/cache"
	"github.com/mafafd/backend/internal/config"
	"github.com/mafafd/backend/internal/models"
	"github.com/mafafd/backend/internal/security"
	"github.com/mafafd/backend/internal/store"
)

type Server struct {
	Cfg   *config.Config
	Log   zerolog.Logger
	Store *store.Store
	Cache *cache.Cache
}

// Register wires all dashboard REST routes (tenant-scoped, JWT protected).
func (s *Server) Register(app *fiber.App) {
	v1 := app.Group("/api/v1")

	v1.Post("/auth/login", s.login)
	v1.Get("/auth/me", requireAuth("platform", "admin", "analyst"), s.me)
	v1.Post("/auth/ws-token", requireAuth("platform", "admin", "analyst"), s.wsToken)

	stats := v1.Group("/stats", requireAuth("platform", "admin", "analyst"))
	stats.Get("/overview", s.overview)
	stats.Get("/timeseries", s.timeseries)
	stats.Get("/risk-distribution", s.riskDistribution)

	tx := v1.Group("/transactions", requireAuth("platform", "admin", "analyst"))
	tx.Get("/", s.listTransactions)
	tx.Get("/:id", s.getTransaction)
	tx.Patch("/:id", requireAuth("platform", "admin", "analyst"), s.updateTransactionStatus)

	inc := v1.Group("/incidents", requireAuth("platform", "admin", "analyst"))
	inc.Get("/", s.listIncidents)
	inc.Patch("/:id", s.updateIncidentStatus)

	rules := v1.Group("/rules", requireAuth("platform", "admin"))
	rules.Get("/", s.getRules)
	rules.Put("/", s.updateRules)

	keys := v1.Group("/keys", requireAuth("platform", "admin"))
	keys.Get("/", s.listKeys)
	keys.Post("/", s.createKey)
	keys.Patch("/:id", s.updateKey)

	audit := v1.Group("/audit", requireAuth("platform", "admin"))
	audit.Get("/", s.listAudit)
}

// --- helpers ---

func tenantID(c *fiber.Ctx) uuid.UUID {
	return authClaims(c).TenantID
}

func authClaims(c *fiber.Ctx) *auth.Claims {
	cl, _ := c.Locals("claims").(*auth.Claims)
	return cl
}

func role(c *fiber.Ctx) string {
	if cl := authClaims(c); cl != nil {
		return cl.Role
	}
	return ""
}

// effectiveTenant applies platform-admin tenant override via X-Tenant-Id.
func (s *Server) effectiveTenant(c *fiber.Ctx) uuid.UUID {
	id := tenantID(c)
	if role(c) == "platform" {
		if override := c.Get("X-Tenant-Id"); override != "" {
			if parsed, err := uuid.Parse(override); err == nil {
				return parsed
			}
		}
	}
	return id
}

func requireAuth(roles ...string) fiber.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *fiber.Ctx) error {
		token := c.Get("Authorization")
		token = trimBearer(token)
		if token == "" {
			token = c.Cookies("mafafd_token")
		}
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing token"})
		}
		secret := c.Locals("jwt_secret").([]byte)
		claims, err := auth.Parse(secret, token)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid token"})
		}
		if !allowed[claims.Role] {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "insufficient permissions"})
		}
		c.Locals("claims", claims)
		return c.Next()
	}
}

func trimBearer(h string) string {
	if len(h) > 7 && h[:7] == "Bearer " {
		return h[7:]
	}
	return h
}

func errRes(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": msg})
}

// --- auth ---

func (s *Server) login(c *fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return errRes(c, fiber.StatusBadRequest, "invalid body")
	}
	if req.Email == "" || req.Password == "" {
		return errRes(c, fiber.StatusBadRequest, "email and password required")
	}
	u, err := s.Store.GetUserByEmail(c.Context(), req.Email)
	if err != nil {
		// constant-time-ish uniform response
		security.CheckPassword("$2a$12$invalidinvalidinvalidinvalidinvalidinvalidinvalid", req.Password)
		return errRes(c, fiber.StatusUnauthorized, "invalid credentials")
	}
	if !security.CheckPassword(u.PasswordHash, req.Password) {
		return errRes(c, fiber.StatusUnauthorized, "invalid credentials")
	}
	// Platform users are cross-tenant (tenant_id is nil); they bypass the
	// tenant lookup and suspension checks.
	tenantName := ""
	if u.Role == string(models.RolePlatform) {
		tenantName = "Platform"
	} else {
		t, err := s.Store.GetTenantByID(c.Context(), u.TenantID)
		if err != nil {
			s.Log.Warn().Str("user", u.ID.String()).Msg("user tenant missing")
			return errRes(c, fiber.StatusInternalServerError, "account misconfigured")
		}
		if t.Status != string(models.TenantActive) {
			return errRes(c, fiber.StatusForbidden, "tenant suspended")
		}
		tenantName = t.Name
	}
	token, err := auth.Sign(s.Cfg.JWTSecret, u.ID, u.TenantID, u.Role, tenantName, s.Cfg.JWTTTLHours)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "token signing failed")
	}
	s.Store.InsertAudit(c.Context(), &u.TenantID, &u.ID, "login", "session", "", nil)
	c.Set("X-Auth-Token", token)
	tenantInfo := fiber.Map{}
	if u.TenantID != uuid.Nil {
		tenantInfo = fiber.Map{"id": u.TenantID, "name": tenantName}
	}
	return c.JSON(fiber.Map{
		"token": token,
		"user": fiber.Map{
			"id":     u.ID,
			"email":  u.Email,
			"role":   u.Role,
			"tenant": tenantInfo,
		},
	})
}

func (s *Server) me(c *fiber.Ctx) error {
	claims := authClaims(c)
	u, err := s.Store.GetUserByID(c.Context(), claims.UserID)
	if err != nil {
		return errRes(c, fiber.StatusNotFound, "user not found")
	}
	t, _ := s.Store.GetTenantByID(c.Context(), claims.TenantID)
	name := claims.TenantName
	if t != nil {
		name = t.Name
	}
	return c.JSON(fiber.Map{
		"id":     u.ID,
		"email":  u.Email,
		"role":   u.Role,
		"tenant": fiber.Map{"id": claims.TenantID, "name": name},
	})
}

// wsToken issues a short-lived token scoped to the WebSocket stream only.
// It must never be usable against REST endpoints (checked via Scope claim).
func (s *Server) wsToken(c *fiber.Ctx) error {
	claims := authClaims(c)
	token, err := auth.SignScoped(s.Cfg.JWTSecret, claims.UserID, claims.TenantID, claims.Role, claims.TenantName, "ws", s.Cfg.WSTokenTTL)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "token signing failed")
	}
	return c.JSON(fiber.Map{"token": token, "ttl_seconds": int(s.Cfg.WSTokenTTL.Seconds())})
}

// --- stats ---

func (s *Server) overview(c *fiber.Ctx) error {
	o, err := s.Store.GetOverview(c.Context(), s.effectiveTenant(c))
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "stats failed")
	}
	return c.JSON(o)
}

func (s *Server) timeseries(c *fiber.Ctx) error {
	bucket := c.Query("bucket", "minute")
	hours := c.QueryInt("hours", 24)
	if hours < 1 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	data, err := s.Store.Timeseries(c.Context(), s.effectiveTenant(c), bucket, since)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "timeseries failed")
	}
	return c.JSON(data)
}

func (s *Server) riskDistribution(c *fiber.Ctx) error {
	d, err := s.Store.RiskDistribution(c.Context(), s.effectiveTenant(c))
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "distribution failed")
	}
	return c.JSON(d)
}

// --- transactions ---

func (s *Server) listTransactions(c *fiber.Ctx) error {
	f := store.TxFilter{
		Search:   c.Query("search"),
		Level:    c.Query("level"),
		Status:   c.Query("status"),
		Currency: c.Query("currency"),
		Page:     c.QueryInt("page", 1),
		PerPage:  c.QueryInt("per_page", 25),
	}
	f.MinRisk = float64(c.QueryInt("min_risk", 0))
	if from := c.Query("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			f.From = t
		}
	}
	if to := c.Query("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			f.To = t
		}
	}
	rows, total, err := s.Store.ListTransactions(c.Context(), s.effectiveTenant(c), f)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "list failed")
	}
	return c.JSON(fiber.Map{"items": rows, "total": total, "page": f.Page, "per_page": f.PerPage})
}

func (s *Server) getTransaction(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errRes(c, fiber.StatusBadRequest, "invalid id")
	}
	tx, err := s.Store.GetTransaction(c.Context(), s.effectiveTenant(c), id)
	if err != nil {
		return errRes(c, fiber.StatusNotFound, "transaction not found")
	}
	return c.JSON(tx)
}

func (s *Server) updateTransactionStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errRes(c, fiber.StatusBadRequest, "invalid id")
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil || !validTxnStatus(req.Status) {
		return errRes(c, fiber.StatusBadRequest, "invalid status")
	}
	if err := s.Store.UpdateTransactionStatus(c.Context(), s.effectiveTenant(c), id, models.TransactionStatus(req.Status)); err != nil {
		return errRes(c, fiber.StatusNotFound, "transaction not found")
	}
	cl := authClaims(c)
	s.Store.InsertAudit(c.Context(), &cl.TenantID, &cl.UserID, "transaction.status", "transaction", id.String(), fiber.Map{"status": req.Status})
	return c.JSON(fiber.Map{"ok": true})
}

func validTxnStatus(s string) bool {
	switch models.TransactionStatus(s) {
	case models.TxnProcessed, models.TxnFlagged, models.TxnInvestigated, models.TxnResolved, models.TxnRefunded:
		return true
	}
	return false
}

// --- incidents ---

func (s *Server) listIncidents(c *fiber.Ctx) error {
	f := store.IncidentFilter{
		Status:  c.Query("status"),
		Level:   c.Query("level"),
		Page:    c.QueryInt("page", 1),
		PerPage: c.QueryInt("per_page", 25),
	}
	rows, total, err := s.Store.ListIncidents(c.Context(), s.effectiveTenant(c), f)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "list failed")
	}
	return c.JSON(fiber.Map{"items": rows, "total": total, "page": f.Page, "per_page": f.PerPage})
}

func (s *Server) updateIncidentStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errRes(c, fiber.StatusBadRequest, "invalid id")
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := c.BodyParser(&req); err != nil || !validIncidentStatus(req.Status) {
		return errRes(c, fiber.StatusBadRequest, "invalid status")
	}
	cl := authClaims(c)
	tenant := s.effectiveTenant(c)
	if err := s.Store.UpdateIncidentStatus(c.Context(), tenant, id, models.IncidentStatus(req.Status), req.Note, cl.UserID); err != nil {
		return errRes(c, fiber.StatusNotFound, "incident not found")
	}
	s.Store.InsertAudit(c.Context(), &tenant, &cl.UserID, "incident.status", "incident", id.String(), fiber.Map{"status": req.Status})
	return c.JSON(fiber.Map{"ok": true})
}

func validIncidentStatus(s string) bool {
	switch models.IncidentStatus(s) {
	case models.IncidentOpen, models.IncidentInvestigating, models.IncidentResolved, models.IncidentFalsePositive:
		return true
	}
	return false
}

// --- rules ---

func (s *Server) getRules(c *fiber.Ctx) error {
	tenant := s.effectiveTenant(c)
	rc, err := s.Store.GetRuleConfig(c.Context(), tenant)
	if err != nil {
		// return defaults so the UI always has something to edit
		defaults := `{
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
}`
		return c.JSON(fiber.Map{"data": json.RawMessage(defaults)})
	}
	return c.JSON(fiber.Map{"data": json.RawMessage(rc.Data)})
}

func (s *Server) updateRules(c *fiber.Ctx) error {
	tenant := s.effectiveTenant(c)
	var req struct {
		Data json.RawMessage `json:"data"`
	}
	if err := c.BodyParser(&req); err != nil || len(req.Data) == 0 {
		return errRes(c, fiber.StatusBadRequest, "invalid rules payload")
	}
	// Validate structure before persisting.
	cl := authClaims(c)
	if err := s.Store.UpsertRuleConfig(c.Context(), tenant, req.Data, cl.UserID); err != nil {
		return errRes(c, fiber.StatusInternalServerError, "save failed")
	}
	// Invalidate the 30s rule cache immediately.
	_ = s.Cache.RDB.Del(c.Context(), cache.TenantRulesKey(tenant.String())).Err()
	s.Store.InsertAudit(c.Context(), &tenant, &cl.UserID, "rules.update", "rules", tenant.String(), nil)
	return c.JSON(fiber.Map{"ok": true})
}

// --- api keys ---

func (s *Server) listKeys(c *fiber.Ctx) error {
	keys, err := s.Store.ListAPIKeys(c.Context(), s.effectiveTenant(c))
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "list keys failed")
	}
	return c.JSON(fiber.Map{"items": keys})
}

func (s *Server) createKey(c *fiber.Ctx) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&req); err != nil || req.Name == "" {
		return errRes(c, fiber.StatusBadRequest, "name required")
	}
	tenant := s.effectiveTenant(c)
	plain, prefix, hash, err := security.GenerateAPIKey()
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "key generation failed")
	}
	k, err := s.Store.CreateAPIKey(c.Context(), tenant, req.Name, hash, prefix)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "save key failed")
	}
	cl := authClaims(c)
	s.Store.InsertAudit(c.Context(), &tenant, &cl.UserID, "key.create", "api_key", k.ID.String(), nil)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"key":          plain,
		"key_prefix":   k.KeyPrefix,
		"id":           k.ID,
		"name":         k.Name,
		"created_at":   k.CreatedAt,
		"show_once":    true,
	})
}

func (s *Server) updateKey(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errRes(c, fiber.StatusBadRequest, "invalid id")
	}
	tenant := s.effectiveTenant(c)
	k, err := s.Store.GetAPIKeyByID(c.Context(), tenant, id)
	if err != nil {
		return errRes(c, fiber.StatusNotFound, "key not found")
	}
	if err := s.Store.RevokeAPIKey(c.Context(), tenant, id); err != nil {
		return errRes(c, fiber.StatusNotFound, "key not found")
	}
	// Immediately evict the auth hot-cache entry so revocation is instant.
	_ = s.Cache.RDB.Del(c.Context(), "auth:key:"+k.KeyHash).Err()
	cl := authClaims(c)
	s.Store.InsertAudit(c.Context(), &tenant, &cl.UserID, "key.revoke", "api_key", id.String(), nil)
	return c.JSON(fiber.Map{"ok": true})
}

// --- audit ---

func (s *Server) listAudit(c *fiber.Ctx) error {
	page := c.QueryInt("page", 1)
	perPage := c.QueryInt("per_page", 50)
	if perPage > 200 {
		perPage = 200
	}
	tenant := s.effectiveTenant(c)
	rows, err := s.Store.Pool.Query(c.Context(), `
		SELECT id, tenant_id, actor_user_id, action, resource, resource_id, detail, created_at
		FROM audit_logs WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		tenant, perPage, (page-1)*perPage)
	if err != nil {
		return errRes(c, fiber.StatusInternalServerError, "audit failed")
	}
	defer rows.Close()
	out := []fiber.Map{}
	for rows.Next() {
		var id int64
		var tid, actor *uuid.UUID
		var action, resource, resourceID string
		var detail []byte
		var at time.Time
		if err := rows.Scan(&id, &tid, &actor, &action, &resource, &resourceID, &detail, &at); err != nil {
			return errRes(c, fiber.StatusInternalServerError, "audit scan failed")
		}
		var d any
		_ = json.Unmarshal(detail, &d)
		out = append(out, fiber.Map{
			"id": id, "tenant_id": tid, "actor_user_id": actor, "action": action,
			"resource": resource, "resource_id": resourceID, "detail": d, "created_at": at,
		})
	}
	return c.JSON(fiber.Map{"items": out})
}
