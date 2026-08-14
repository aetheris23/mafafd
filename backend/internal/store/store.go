package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/mafafd/backend/internal/models"
)

// Store wraps a pgx connection pool with typed data access.
type Store struct {
	Pool *pgxpool.Pool
	Log  zerolog.Logger
}

func New(ctx context.Context, url string, log zerolog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{Pool: pool, Log: log}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Ping returns nil when the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// ---------------------------------------------------------------------------
// Tenants
// ---------------------------------------------------------------------------

func (s *Store) GetTenantByID(ctx context.Context, id uuid.UUID) (*models.Tenant, error) {
	var t models.Tenant
	err := s.Pool.QueryRow(ctx, `
		SELECT id, name, status, base_currency, settings, created_at
		FROM tenants WHERE id = $1`, id).
		Scan(&t.ID, &t.Name, &t.Status, &t.BaseCurrency, &t.Settings, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return &t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, name, status, base_currency, settings, created_at
		FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	out := []models.Tenant{}
	for rows.Next() {
		var t models.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.BaseCurrency, &t.Settings, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTenantByAPIKeyHash resolves a tenant from a hashed ingestion API key.
func (s *Store) GetTenantByAPIKeyHash(ctx context.Context, keyHash string) (*models.Tenant, *models.APIKey, error) {
	var t models.Tenant
	var k models.APIKey
	err := s.Pool.QueryRow(ctx, `
		SELECT t.id, t.name, t.status, t.base_currency, t.settings, t.created_at,
		       k.id, k.tenant_id, k.name, k.key_prefix, k.last_used_at, k.revoked_at, k.created_at
		FROM api_keys k
		JOIN tenants t ON t.id = k.tenant_id
		WHERE k.key_hash = $1 AND k.revoked_at IS NULL`, keyHash).
		Scan(&t.ID, &t.Name, &t.Status, &t.BaseCurrency, &t.Settings, &t.CreatedAt,
			&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("lookup api key: %w", err)
	}
	if t.Status != string(models.TenantActive) {
		return &t, &k, ErrTenantSuspended
	}
	return &t, &k, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, keyID uuid.UUID) {
	_, _ = s.Pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, keyID)
}

// ---------------------------------------------------------------------------
// API Keys (dashboard managed)
// ---------------------------------------------------------------------------

func (s *Store) CreateAPIKey(ctx context.Context, tenantID uuid.UUID, name, hash, prefix string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, name, key_hash, key_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, name, key_prefix, last_used_at, revoked_at, created_at`,
		tenantID, name, hash, prefix).
		Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return &k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, tenantID uuid.UUID) ([]models.APIKey, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, tenant_id, name, key_prefix, last_used_at, revoked_at, created_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	out := []models.APIKey{}
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyPrefix, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) RevokeAPIKey(ctx context.Context, tenantID, keyID uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND tenant_id = $2 AND revoked_at IS NULL`,
		keyID, tenantID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAPIKeyByID(ctx context.Context, tenantID, keyID uuid.UUID) (*models.APIKey, error) {
	var k models.APIKey
	err := s.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, key_hash, key_prefix, last_used_at, revoked_at, created_at
		FROM api_keys WHERE id = $1 AND tenant_id = $2`, keyID, tenantID).
		Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return &k, nil
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	var u models.User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, created_at
		FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	err := s.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, password_hash, role, created_at
		FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, tenantID uuid.UUID, email, hash string, role models.UserRole) (*models.User, error) {
	var u models.User
	var tenantArg any
	if tenantID == uuid.Nil {
		tenantArg = nil
	} else {
		tenantArg = tenantID
	}
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, email, role, created_at`,
		tenantArg, email, hash, string(role)).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// Transactions
// ---------------------------------------------------------------------------

const txnCols = `id, tenant_id, external_id, amount, currency, customer_id_hash,
	customer_email_hash, card_token, ip_address, device_fingerprint_hash, country_code,
	city, merchant_category, metadata, risk_score, risk_level, deterministic_score,
	ai_score, ai_verdict, ai_explanation, rule_breakdown, is_anomaly, status,
	transaction_time, created_at, processed_at`

type txnScan struct {
	tx *models.Transaction
}

func scanTxn(row pgx.Row) (*models.Transaction, error) {
	t := &models.Transaction{}
	var metadata, breakdown []byte
	err := row.Scan(&t.ID, &t.TenantID, &t.ExternalID, &t.Amount, &t.Currency,
		&t.CustomerIDHash, &t.CustomerEmailHash, &t.CardToken, &t.IPAddress,
		&t.DeviceFingerprintHash, &t.CountryCode, &t.City, &t.MerchantCategory,
		&metadata, &t.RiskScore, &t.RiskLevel, &t.DeterministicScore,
		&t.AIScore, &t.AIVerdict, &t.AIExplanation, &breakdown, &t.IsAnomaly,
		&t.Status, &t.TransactionTime, &t.CreatedAt, &t.ProcessedAt)
	if err != nil {
		return nil, err
	}
	t.Metadata = map[string]any{}
	_ = json.Unmarshal(metadata, &t.Metadata)
	t.RuleBreakdown = []models.RuleScore{}
	_ = json.Unmarshal(breakdown, &t.RuleBreakdown)
	return t, nil
}

func (s *Store) InsertTransaction(ctx context.Context, t *models.Transaction) error {
	metadata, _ := json.Marshal(t.Metadata)
	breakdown, _ := json.Marshal(t.RuleBreakdown)
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO transactions (id, tenant_id, external_id, amount, currency,
			customer_id_hash, customer_email_hash, card_token, ip_address,
			device_fingerprint_hash, country_code, city, merchant_category, metadata,
			risk_score, risk_level, deterministic_score, ai_score, ai_verdict,
			ai_explanation, rule_breakdown, is_anomaly, status, transaction_time, processed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
		ON CONFLICT (id) DO NOTHING`,
		t.ID, t.TenantID, t.ExternalID, t.Amount, t.Currency,
		t.CustomerIDHash, t.CustomerEmailHash, t.CardToken, t.IPAddress,
		t.DeviceFingerprintHash, t.CountryCode, t.City, t.MerchantCategory, metadata,
		t.RiskScore, string(t.RiskLevel), t.DeterministicScore, t.AIScore, t.AIVerdict,
		t.AIExplanation, breakdown, t.IsAnomaly, string(t.Status), t.TransactionTime, t.ProcessedAt)
	return err
}

func (s *Store) GetTransaction(ctx context.Context, tenantID, id uuid.UUID) (*models.Transaction, error) {
	return scanTxn(s.Pool.QueryRow(ctx, `
		SELECT `+txnCols+` FROM transactions WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

func (s *Store) ListTransactions(ctx context.Context, tenantID uuid.UUID, f TxFilter) ([]models.Transaction, int64, error) {
	where := "WHERE tenant_id = $1"
	args := []any{tenantID}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where += fmt.Sprintf(" AND (external_id ILIKE $%d OR card_token ILIKE $%d OR customer_email_hash ILIKE $%d)", len(args), len(args), len(args))
	}
	if f.MinRisk > 0 {
		args = append(args, f.MinRisk)
		where += fmt.Sprintf(" AND risk_score >= $%d", len(args))
	}
	if f.Level != "" {
		args = append(args, f.Level)
		where += fmt.Sprintf(" AND risk_level = $%d", len(args))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if f.Currency != "" {
		args = append(args, f.Currency)
		where += fmt.Sprintf(" AND currency = $%d", len(args))
	}
	if !f.From.IsZero() {
		args = append(args, f.From)
		where += fmt.Sprintf(" AND transaction_time >= $%d", len(args))
	}
	if !f.To.IsZero() {
		args = append(args, f.To)
		where += fmt.Sprintf(" AND transaction_time <= $%d", len(args))
	}

	var total int64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM transactions `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 || f.PerPage > 100 {
		f.PerPage = 25
	}
	limit := f.PerPage
	offset := (f.Page - 1) * f.PerPage
	args = append(args, limit, offset)
	q := `SELECT ` + txnCols + ` FROM transactions ` + where +
		` ORDER BY transaction_time DESC LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Transaction{}
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateTransactionAI(ctx context.Context, id uuid.UUID, score float64, level models.RiskLevel, verdict, explanation string) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE transactions
		SET risk_score = $2, risk_level = $3, ai_score = $2, ai_verdict = $4,
		    ai_explanation = $5, is_anomaly = $6
		WHERE id = $1`,
		id, score, string(level), verdict, explanation, score >= 40)
	return err
}

func (s *Store) UpdateTransactionStatus(ctx context.Context, tenantID, id uuid.UUID, status models.TransactionStatus) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE transactions SET status = $3 WHERE id = $1 AND tenant_id = $2`, id, tenantID, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Incidents
// ---------------------------------------------------------------------------

func (s *Store) InsertIncident(ctx context.Context, in *models.Incident) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO anomaly_incidents (id, tenant_id, transaction_id, risk_score, risk_level,
			reason, ai_explanation, status, note, decided_by, decided_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (transaction_id) DO UPDATE SET
			risk_score = EXCLUDED.risk_score,
			risk_level = EXCLUDED.risk_level,
			reason = EXCLUDED.reason,
			ai_explanation = COALESCE(EXCLUDED.ai_explanation, anomaly_incidents.ai_explanation),
			updated_at = now()`,
		in.ID, in.TenantID, in.TransactionID, in.RiskScore, string(in.RiskLevel),
		in.Reason, in.AIExplanation, string(in.Status), in.Note, in.DecidedBy, in.DecidedAt)
	return err
}

func (s *Store) ListIncidents(ctx context.Context, tenantID uuid.UUID, f IncidentFilter) ([]models.Incident, int64, error) {
	where := "WHERE i.tenant_id = $1"
	args := []any{tenantID}
	if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND i.status = $%d", len(args))
	}
	if f.Level != "" {
		args = append(args, f.Level)
		where += fmt.Sprintf(" AND i.risk_level = $%d", len(args))
	}

	var total int64
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM anomaly_incidents i `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 || f.PerPage > 100 {
		f.PerPage = 25
	}
	args = append(args, f.PerPage, (f.Page-1)*f.PerPage)
	q := `
		SELECT i.id, i.tenant_id, i.transaction_id, i.risk_score, i.risk_level, i.reason,
		       i.ai_explanation, i.status, i.note, i.decided_by, i.decided_at, i.created_at, i.updated_at
		FROM anomaly_incidents i ` + where +
		` ORDER BY i.created_at DESC LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.Incident{}
	for rows.Next() {
		var inc models.Incident
		if err := rows.Scan(&inc.ID, &inc.TenantID, &inc.TransactionID, &inc.RiskScore,
			&inc.RiskLevel, &inc.Reason, &inc.AIExplanation, &inc.Status, &inc.Note,
			&inc.DecidedBy, &inc.DecidedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, inc)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateIncidentStatus(ctx context.Context, tenantID, id uuid.UUID, status models.IncidentStatus, note string, decidedBy uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE anomaly_incidents
		SET status = $3, note = COALESCE($4, note), decided_by = $5, decided_at = now(), updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, id, tenantID, string(status), note, decidedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rule configs (dynamic thresholds)
// ---------------------------------------------------------------------------

func (s *Store) GetRuleConfig(ctx context.Context, tenantID uuid.UUID) (*models.RuleConfig, error) {
	var rc models.RuleConfig
	err := s.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, data, updated_by, updated_at
		FROM rule_configs WHERE tenant_id = $1`, tenantID).
		Scan(&rc.ID, &rc.TenantID, &rc.Data, &rc.UpdatedBy, &rc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get rule config: %w", err)
	}
	return &rc, nil
}

func (s *Store) UpsertRuleConfig(ctx context.Context, tenantID uuid.UUID, data []byte, updatedBy uuid.UUID) error {
	var byArg any
	if updatedBy == uuid.Nil {
		byArg = nil
	} else {
		byArg = updatedBy
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO rule_configs (tenant_id, data, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (tenant_id) DO UPDATE SET data = EXCLUDED.data, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		tenantID, data, byArg)
	return err
}

// ---------------------------------------------------------------------------
// Statistics
// ---------------------------------------------------------------------------

type Overview struct {
	TxTotal          int64     `json:"tx_total"`
	TxLastMinute     int64     `json:"tx_last_minute"`
	TxLastHour       int64     `json:"tx_last_hour"`
	AnomalyCount     int64     `json:"anomaly_count"`
	OpenIncidents    int64     `json:"open_incidents"`
	AvgRisk          float64   `json:"avg_risk"`
	HighRiskCount    int64     `json:"high_risk_count"`
	ProcessedCount   int64     `json:"processed_count"`
	DeterministicCount int64   `json:"deterministic_count"`
	AICount          int64     `json:"ai_count"`
	LastProcessed    time.Time `json:"last_processed"`
}

func (s *Store) GetOverview(ctx context.Context, tenantID uuid.UUID) (*Overview, error) {
	o := &Overview{}
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE transaction_time > now() - interval '1 minute'),
		       count(*) FILTER (WHERE transaction_time > now() - interval '1 hour'),
		       count(*) FILTER (WHERE is_anomaly),
		       count(*) FILTER (WHERE risk_level = 'HIGH'),
		       coalesce(avg(risk_score), 0),
		       max(created_at)
		FROM transactions WHERE tenant_id = $1`, tenantID).
		Scan(&o.TxLastMinute, &o.TxLastHour, &o.AnomalyCount, &o.HighRiskCount, &o.AvgRisk, &o.LastProcessed); err != nil {
		return nil, err
	}
	if err := s.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM transactions WHERE tenant_id = $1),
		       (SELECT count(*) FROM anomaly_incidents WHERE tenant_id = $1 AND status = 'open'),
		       (SELECT count(*) FROM transactions WHERE tenant_id = $1 AND ai_score IS NOT NULL)`, tenantID).
		Scan(&o.TxTotal, &o.OpenIncidents, &o.AICount); err != nil {
		return nil, err
	}
	return o, nil
}

type Bucket struct {
	Ts            time.Time `json:"ts"`
	Count         int64     `json:"count"`
	Anomalies     int64     `json:"anomalies"`
	AvgRisk       float64   `json:"avg_risk"`
	SumAmount     float64   `json:"sum_amount"`
}

func (s *Store) Timeseries(ctx context.Context, tenantID uuid.UUID, bucket string, since time.Time) ([]Bucket, error) {
	step := "minute"
	switch bucket {
	case "hour":
		step = "hour"
	case "day":
		step = "day"
	}
	q := fmt.Sprintf(`
		SELECT date_trunc('%s', transaction_time) AS ts,
		       count(*),
		       count(*) FILTER (WHERE is_anomaly),
		       coalesce(avg(risk_score), 0),
		       coalesce(sum(amount), 0)
		FROM transactions
		WHERE tenant_id = $1 AND transaction_time >= $2
		GROUP BY ts ORDER BY ts ASC`, step)
	rows, err := s.Pool.Query(ctx, q, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Ts, &b.Count, &b.Anomalies, &b.AvgRisk, &b.SumAmount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RiskDistribution returns risk level counts for a pie chart.
func (s *Store) RiskDistribution(ctx context.Context, tenantID uuid.UUID) (map[string]int64, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT risk_level, count(*) FROM transactions
		WHERE tenant_id = $1 AND transaction_time > now() - interval '7 days'
		GROUP BY risk_level`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var lvl string
		var n int64
		if err := rows.Scan(&lvl, &n); err != nil {
			return nil, err
		}
		out[lvl] = n
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

func (s *Store) InsertAudit(ctx context.Context, tenantID, actor *uuid.UUID, action, resource, resourceID string, detail any) {
	b, _ := json.Marshal(detail)
	_, _ = s.Pool.Exec(ctx, `
		INSERT INTO audit_logs (tenant_id, actor_user_id, action, resource, resource_id, detail)
		VALUES ($1,$2,$3,$4,$5,$6)`, tenantID, actor, action, resource, resourceID, b)
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

type TxFilter struct {
	Search  string
	MinRisk float64
	Level   string
	Status  string
	Currency string
	From    time.Time
	To      time.Time
	Page    int
	PerPage int
}

type IncidentFilter struct {
	Status  string
	Level   string
	Page    int
	PerPage int
}

var (
	ErrNotFound       = errors.New("not found")
	ErrTenantSuspended = errors.New("tenant suspended")
)

// TransactionBatch enables fast bulk seeding.
type TransactionBatch struct {
	Transactions []models.Transaction
	Incidents    []models.Incident
}

func (s *Store) BatchInsert(ctx context.Context, b *TransactionBatch) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, t := range b.Transactions {
		metadata, _ := json.Marshal(t.Metadata)
		breakdown, _ := json.Marshal(t.RuleBreakdown)
		if _, err := tx.Exec(ctx, `
			INSERT INTO transactions (id, tenant_id, external_id, amount, currency,
				customer_id_hash, customer_email_hash, card_token, ip_address,
				device_fingerprint_hash, country_code, city, merchant_category, metadata,
				risk_score, risk_level, deterministic_score, ai_score, ai_verdict,
				ai_explanation, rule_breakdown, is_anomaly, status, transaction_time, created_at, processed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
			ON CONFLICT (id) DO NOTHING`,
			t.ID, t.TenantID, t.ExternalID, t.Amount, t.Currency,
			t.CustomerIDHash, t.CustomerEmailHash, t.CardToken, t.IPAddress,
			t.DeviceFingerprintHash, t.CountryCode, t.City, t.MerchantCategory, metadata,
			t.RiskScore, string(t.RiskLevel), t.DeterministicScore, t.AIScore, t.AIVerdict,
			t.AIExplanation, breakdown, t.IsAnomaly, string(t.Status), t.TransactionTime, t.CreatedAt, t.ProcessedAt); err != nil {
			return err
		}
	}
	for _, inc := range b.Incidents {
		if _, err := tx.Exec(ctx, `
			INSERT INTO anomaly_incidents (id, tenant_id, transaction_id, risk_score, risk_level,
				reason, ai_explanation, status, note, decided_by, decided_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (transaction_id) DO NOTHING`,
			inc.ID, inc.TenantID, inc.TransactionID, inc.RiskScore, string(inc.RiskLevel),
			inc.Reason, inc.AIExplanation, string(inc.Status), inc.Note, inc.DecidedBy, inc.DecidedAt,
			inc.CreatedAt, inc.UpdatedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
