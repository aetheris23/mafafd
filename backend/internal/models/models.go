package models

import (
	"time"

	"github.com/google/uuid"
)

type RiskLevel string

const (
	RiskLow        RiskLevel = "LOW"
	RiskSuspicious RiskLevel = "SUSPICIOUS"
	RiskHigh       RiskLevel = "HIGH"
)

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

type TransactionStatus string

const (
	TxnProcessed    TransactionStatus = "processed"
	TxnFlagged      TransactionStatus = "flagged"
	TxnInvestigated TransactionStatus = "investigated"
	TxnResolved     TransactionStatus = "resolved"
	TxnRefunded     TransactionStatus = "refunded"
)

type IncidentStatus string

const (
	IncidentOpen          IncidentStatus = "open"
	IncidentInvestigating IncidentStatus = "investigating"
	IncidentResolved      IncidentStatus = "resolved"
	IncidentFalsePositive IncidentStatus = "false_positive"
)

type UserRole string

const (
	RolePlatform UserRole = "platform"
	RoleAdmin    UserRole = "admin"
	RoleAnalyst  UserRole = "analyst"
)

// Tenant is a single isolated customer of the platform.
type Tenant struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	BaseCurrency string    `json:"base_currency"`
	Settings     []byte    `json:"settings"`
	CreatedAt    time.Time `json:"created_at"`
}

// APIKey is the secret credential a merchant uses to call the ingestion API.
// Only the SHA-256 hash is stored; the plaintext is shown exactly once on creation.
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	KeyPrefix  string     `json:"key_prefix"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type User struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

// TransactionInput is the payload accepted by the ingestion API.
type TransactionInput struct {
	ExternalID       string                 `json:"external_id"`
	Amount           float64                `json:"amount"`
	Currency         string                 `json:"currency"`
	CustomerID       string                 `json:"customer_id"`
	CustomerEmail    string                 `json:"customer_email"`
	CardToken        string                 `json:"card_token"`
	IPAddress        string                 `json:"ip_address"`
	DeviceFingerprint string                `json:"device_fingerprint"`
	CountryCode      string                 `json:"country_code"`
	City             string                 `json:"city"`
	MerchantCategory string                 `json:"merchant_category"`
	Metadata         map[string]interface{} `json:"metadata"`
	Timestamp        *time.Time             `json:"timestamp"`
}

// RuleScore records a single deterministic check result.
type RuleScore struct {
	Rule    string  `json:"rule"`
	Score   float64 `json:"score"`
	Weight  float64 `json:"weight"`
	Detail  string  `json:"detail"`
}

// Transaction is the stored/published canonical record.
type Transaction struct {
	ID                uuid.UUID         `json:"id"`
	TenantID          uuid.UUID         `json:"tenant_id"`
	ExternalID        string            `json:"external_id"`
	Amount            float64           `json:"amount"`
	Currency          string            `json:"currency"`
	CustomerIDHash    string            `json:"customer_id_hash"`
	CustomerEmailHash string            `json:"customer_email_hash"`
	CardToken         string            `json:"card_token"`
	IPAddress         string            `json:"ip_address"`
	DeviceFingerprintHash string        `json:"device_fingerprint_hash"`
	CountryCode       string            `json:"country_code"`
	City              string            `json:"city"`
	MerchantCategory  string            `json:"merchant_category"`
	Metadata          map[string]any    `json:"metadata"`
	RiskScore         float64           `json:"risk_score"`
	RiskLevel         RiskLevel         `json:"risk_level"`
	DeterministicScore float64          `json:"deterministic_score"`
	AIScore           *float64          `json:"ai_score,omitempty"`
	AIVerdict         string            `json:"ai_verdict,omitempty"`
	AIExplanation     string            `json:"ai_explanation,omitempty"`
	RuleBreakdown     []RuleScore       `json:"rule_breakdown"`
	IsAnomaly         bool              `json:"is_anomaly"`
	Status            TransactionStatus `json:"status"`
	TransactionTime   time.Time         `json:"transaction_time"`
	CreatedAt         time.Time         `json:"created_at"`
	ProcessedAt       *time.Time        `json:"processed_at,omitempty"`
}

// Incident is a flagged anomaly requiring analyst attention.
type Incident struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      uuid.UUID       `json:"tenant_id"`
	TransactionID uuid.UUID       `json:"transaction_id"`
	RiskScore     float64         `json:"risk_score"`
	RiskLevel     RiskLevel       `json:"risk_level"`
	Reason        string          `json:"reason"`
	AIExplanation string          `json:"ai_explanation,omitempty"`
	Status        IncidentStatus  `json:"status"`
	Note          string          `json:"note,omitempty"`
	DecidedBy     *uuid.UUID      `json:"decided_by,omitempty"`
	DecidedAt     *time.Time      `json:"decided_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Transaction   *Transaction    `json:"transaction,omitempty"`
}

// RuleConfig is the dynamic per-tenant deterministic engine configuration.
type RuleConfig struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Data      []byte    `json:"data"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Event is the envelope published to Redis Pub/Sub for the dashboard WebSocket.
type Event struct {
	Event string    `json:"event"`
	Data  any       `json:"data"`
	At    time.Time `json:"at"`
}

// AITask wraps a suspicious transaction for the asynchronous AI pipeline.
type AITask struct {
	Transaction        *Transaction `json:"transaction"`
	VelocityCounts     map[string]int64 `json:"velocity_counts"`
	DeterministicScore float64      `json:"deterministic_score"`
	Reason             string       `json:"reason"`
}
