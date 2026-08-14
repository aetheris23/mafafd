-- ============================================================================
-- MAFAFD - Monthly table partitioning for transactions (maintenance task)
-- Docs TDD section 4.B: "Database Maintenance: monthly partitioning"
--
-- In production (high volume), run this to convert transactions to a
-- partitioned parent. Apply to a fresh DB before heavy load, or use the
-- `pg_partman` extension for automatic partition creation.
-- ============================================================================

-- 1) Create the partitioned parent
CREATE TABLE IF NOT EXISTS transactions_partitioned (
    LIKE transactions INCLUDING ALL
) PARTITION BY RANGE (transaction_time);

-- 2) Create partitions for the current month
CREATE TABLE IF NOT EXISTS transactions_y2026m08 PARTITION OF transactions_partitioned
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

-- 3) Attach old data
-- ALTER TABLE transactions_partitioned ATTACH PARTITION transactions_y2026m07
--    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

-- Maintenance checklist (schedule via cron / pg_cron):
--   * create NEXT month partition:
--       CREATE TABLE IF NOT EXISTS transactions_y2026m09 PARTITION OF transactions_partitioned
--           FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
--   * archive data older than 6 months to cold storage (GCS/S3) and DETACH.
