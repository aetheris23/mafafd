-- ============================================================================
-- MAFAFD - Multi-Tenant AI Financial Anomaly & Fraud Detector
-- PostgreSQL schema
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------------------------------------------------------------------------
-- tenants: one row per merchant (isolation boundary)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    base_currency CHAR(3) NOT NULL DEFAULT 'USD',
    settings      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- api_keys: hashed merchant credentials (plaintext shown exactly once)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,          -- SHA-256 of the secret
    key_prefix  TEXT NOT NULL,                 -- display hint, e.g. sk_live_ab12
    last_used_at TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);

-- ---------------------------------------------------------------------------
-- users: dashboard identities (role-scoped, tenant-scoped)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID REFERENCES tenants(id) ON DELETE CASCADE,  -- NULL => platform role
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'analyst' CHECK (role IN ('platform','admin','analyst')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- transactions: canonical masked transaction record
-- NOTE: table partitioning (monthly) is applied by db/ops/02_partition.sql
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS transactions (
    id                     UUID PRIMARY KEY,
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    external_id            TEXT,
    amount                 NUMERIC(20,2) NOT NULL CHECK (amount > 0),
    currency               CHAR(3) NOT NULL,
    customer_id_hash       TEXT,
    customer_email_hash    TEXT,
    card_token             TEXT,                     -- non-reversible hash
    ip_address             TEXT,
    device_fingerprint_hash TEXT,
    country_code           CHAR(2),
    city                   TEXT,
    merchant_category      TEXT,
    metadata               JSONB NOT NULL DEFAULT '{}',
    risk_score             DOUBLE PRECISION NOT NULL DEFAULT 0,
    risk_level             TEXT NOT NULL DEFAULT 'LOW',
    deterministic_score    DOUBLE PRECISION NOT NULL DEFAULT 0,
    ai_score               DOUBLE PRECISION,
    ai_verdict             TEXT,
    ai_explanation         TEXT,
    rule_breakdown         JSONB NOT NULL DEFAULT '[]',
    is_anomaly             BOOLEAN NOT NULL DEFAULT FALSE,
    status                 TEXT NOT NULL DEFAULT 'processed',
    transaction_time       TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at           TIMESTAMPTZ,
    CONSTRAINT ck_risk_level CHECK (risk_level IN ('LOW','SUSPICIOUS','HIGH'))
);
CREATE INDEX IF NOT EXISTS idx_txn_tenant_time   ON transactions (tenant_id, transaction_time DESC);
CREATE INDEX IF NOT EXISTS idx_txn_tenant_risk   ON transactions (tenant_id, risk_score DESC);
CREATE INDEX IF NOT EXISTS idx_txn_tenant_status ON transactions (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_txn_tenant_times  ON transactions (tenant_id, transaction_time);

-- ---------------------------------------------------------------------------
-- anomaly_incidents: analyst triage queue
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS anomaly_incidents (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    transaction_id   UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE UNIQUE,
    risk_score       DOUBLE PRECISION NOT NULL,
    risk_level       TEXT NOT NULL,
    reason           TEXT NOT NULL,
    ai_explanation   TEXT,
    status           TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','investigating','resolved','false_positive')),
    note             TEXT,
    decided_by       UUID REFERENCES users(id),
    decided_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_incident_tenant_status ON anomaly_incidents (tenant_id, status, created_at DESC);

-- ---------------------------------------------------------------------------
-- rule_configs: dynamic deterministic engine thresholds
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS rule_configs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    data       JSONB NOT NULL DEFAULT '{}',
    updated_by UUID REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- audit_logs: append-only security trail
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    UUID,
    actor_user_id UUID,
    action       TEXT NOT NULL,
    resource     TEXT,
    resource_id  TEXT,
    detail       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_logs (tenant_id, created_at DESC);

-- updated_at triggers
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['tenants','users','anomaly_incidents','rule_configs']
    LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_' || t || '_updated_at') THEN
            EXECUTE format('CREATE TRIGGER trg_%s_updated_at BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_updated_at()', t, t);
        END IF;
    END LOOP;
END $$;
