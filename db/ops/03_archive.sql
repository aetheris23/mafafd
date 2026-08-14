-- ============================================================================
-- MAFAFD - Cold storage archival helper (maintenance task)
-- Docs TDD section 4.B: archive transactions older than 6 months to cold
-- storage. This function exports a batch of old rows as JSON for export to
-- GCS/S3 and removes them from the hot table.
--
-- Schedule via cron, e.g. daily at 03:00:
--   SELECT archive_transactions(now() - interval '6 months', 10000);
-- ============================================================================

CREATE OR REPLACE FUNCTION archive_transactions(p_before TIMESTAMPTZ, p_limit INTEGER)
RETURNS TABLE(archived UUID, payload JSONB) AS $$
BEGIN
    RETURN QUERY
    WITH old AS (
        SELECT id, tenant_id, external_id, amount, currency, customer_id_hash,
               customer_email_hash, card_token, ip_address, device_fingerprint_hash,
               country_code, city, merchant_category, metadata, risk_score,
               risk_level, deterministic_score, ai_score, ai_verdict,
               ai_explanation, rule_breakdown, is_anomaly, status,
               transaction_time, created_at, processed_at
        FROM transactions
        WHERE transaction_time < p_before
        ORDER BY transaction_time
        LIMIT p_limit
        FOR UPDATE SKIP LOCKED
    ), del AS (
        DELETE FROM transactions t
        USING old o
        WHERE t.id = o.id
        RETURNING t.id
    )
    SELECT o.id::UUID, row_to_json(o)::JSONB
    FROM old o
    JOIN del d ON d.id = o.id;
END;
$$ LANGUAGE plpgsql;
