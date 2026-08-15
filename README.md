# MAFAFD — Multi-Tenant AI Financial Anomaly & Fraud Detector

A high-throughput, decoupled, async fraud/anomaly detection platform per
`Technical_Design_Document_Anomaly_Detector.docx`.

**Ingestion SLA: < 20 ms.** The ingestion API acknowledges a transaction in a
few milliseconds by **persisting it in Redis** and handing off heavy work to an
async pipeline. Deterministic rules run in a dedicated worker (~ms); AI
assessment (Gemini / OpenAI-compatible / local heuristic fallback) runs
fully async and never blocks the API.

```
Client ──POST──▶ Ingestion (Fiber, :8080)
                  │  API-key auth · HMAC verify · token-bucket ratelimit
                  │  PII masked · Redis persist · respond < 20ms
                  ▼
              RabbitMQ (or Redis Streams / in-memory)
              transaction.new            ai.analysis
                  ▼                          ▲
             worker-rules               worker-ai
              velocity (Redis Lua)        PII-masked prompt
              geo / IP / amount           Gemini | OpenAI | local
                  │                          │
                  ▼                          ▼
               PostgreSQL               anomaly_incidents
                  │  events ──Redis Pub/Sub──▶ gateway (:8081)
                  ▼                             │ REST /metrics + WebSocket
              dashboard (Next.js :3000) ◀───────┘ live stream
```

---

## Architecture

| Component       | Role                                                                 |
|-----------------|----------------------------------------------------------------------|
| `cmd/ingestion` | Fiber HTTP API, `X-Api-Key` auth (SHA-256 lookup), optional HMAC signature, Redis token-bucket rate limit, PII masking at the edge, Redis SET persistence, publishes `transaction.new` |
| `cmd/worker-rules` | Deterministic rules: sliding-window velocity (customer/IP/card/device/global, Redis Lua), geo allow/blocklist, IP blacklist, amount/currency thresholds. Weighted score → LOW (<30) / SUSPICIOUS (30–70, sent to AI) / HIGH (≥70). Incident created at ≥40. Publishes `ai.analysis` |
| `cmd/worker-ai`  | Fetches full txn, masks PII before egress, calls provider (JSON-mode), updates `ai_score`/`ai_explanation`, falls back to local heuristic if provider fails |
| `cmd/gateway`    | Dashboard REST API (JWT, tenant-scoped SQL, audit log), `/metrics`, WebSocket live stream via Redis Pub/Sub relay, short-lived WS-scoped tokens |
| `frontend`       | Next.js 15 dashboard: login, live overview (Recharts), transactions, incidents (AI drawer), API keys, rules editor, audit log. Same-origin `/api/v1/*` rewrite to gateway; httpOnly session cookie; WS uses a separate short-lived scoped token |
| `prometheus`+`grafana` | Service metrics: ingestion latency p95/p99, decision rates, rule/AI latency, queue depth, WS clients |

### Message broker abstraction (`internal/mq`)
- `MQ_PROVIDER=rabbit` — RabbitMQ (optional; needs a RabbitMQ server).
- `MQ_PROVIDER=redis` — Redis Streams (default; no broker needed).
- `MQ_PROVIDER=memory` — in-process (unit tests).

### AI providers (`internal/ai`)
- `AI_PROVIDER=gemini` | `openai` | `local`. `local` uses a transparent
  deterministic heuristic (offline, zero cost) so the whole pipeline runs
  without API keys. `AI_FALLBACK_LOCAL=true` keeps the pipeline alive when a
  provider fails.

---

## Quick start (no Docker)

The whole stack runs natively with plain Go binaries, Redis and PostgreSQL —
no Docker, no RabbitMQ, no API keys required. The default `.env` uses
`MQ_PROVIDER=redis` (Redis Streams as the message broker) and `AI_PROVIDER=local`
(offline heuristic AI).

**Prerequisites:** Go 1.26+, Node 24+, Redis 7.x, PostgreSQL 15+ (`initdb` /
`pg_ctl`), `curl`.

### One command — everything at once

```bash
cp .env.example .env      # first run only
make dev                  # infra + backend + feeder + frontend, all in one shot
```

`make db-reset` drops the `mafafd` database and recreates it (schema applied;
stops backend services first so nothing blocks the drop). `make dev-fresh` does
the same reset and then boots the whole stack, so the demo tenant and realistic
historical transactions (merchant, e-wallet payment method, location) are
re-seeded automatically. Stop everything with `make stop-all`.

### Step-by-step (equivalent, for debugging)

```bash
# 1) Local infrastructure (own Postgres cluster on :5433 + Redis on :6379)
make infra-start          # init + start Postgres, apply schema, start Redis

# 2) Configure
cp .env.example .env      # defaults already point at the local infra above

# 3) Build + start the four backend services (ingestion, gateway, worker-rules, worker-ai)
make run

# 4) Dashboard (Next.js, port 3000)
make frontend-dev
```

If the tools live somewhere else, override the Makefile variables:

```bash
make infra-start REDIS_SERVER=/usr/bin/redis-server PGDATA=~/tools/pgdata PGUSER=evosynx
```

On first boot the gateway seeds a demo tenant, two users and an API key
(`SEED_DEMO_DATA=true`):

| Type      | Credential                                                                 |
|-----------|----------------------------------------------------------------------------|
| Platform  | `platform@mafafd.dev` / `Mafafd-Platform-2026!`                             |
| Tenant    | `admin@acme.dev` / `Acme-Admin-2026!` (tenant: Acme Payments)              |
| API key   | `sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d`     |

| Endpoint         | URL                                        |
|------------------|--------------------------------------------|
| Dashboard        | http://localhost:3000                      |
| Ingestion API    | http://localhost:8080/api/v1/transactions  |
| Gateway API      | http://localhost:8081                      |
| Ingestion health | http://localhost:8080/health               |
| Gateway health   | http://localhost:8081/health               |

### Ingest a transaction

```bash
curl -X POST http://localhost:8080/api/v1/transactions \
  -H "X-Api-Key: sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d" \
  -H "Content-Type: application/json" \
  -d '{"external_id":"t-1001","amount":4500.00,"currency":"USD",
       "customer_id":"c-7781","customer_email":"user@acme.dev",
       "card_token":"tok_visa_abc123","ip_address":"198.51.100.23",
       "device_fingerprint":"dev-fp-1","country_code":"US","city":"NYC",
       "merchant_category":"online_retail","merchant_name":"Amazon",
       "payment_method":"card_visa"}'
```

Response is `202 Accepted` (`estimated_processing_ms` is the full handler time —
the measured p95 is ~10 ms under load). Send `amount: 6000` or repeat the same
customer 6× fast to watch the score climb and an incident appear live on the
dashboard.

### Useful Makefile targets

```bash
make dev                  # one command: infra + backend + feeder + frontend
make db-reset             # drop mafafd DB + recreate (schema applied, re-seed on next boot)
make dev-fresh            # db-reset + dev: clean DB then boot everything
make stop-all             # stop backend services + feeder + frontend + infra
make test                 # go test ./...
make lint                 # go vet ./... + tsc
make run                  # build + start the four backend services
make stop                 # stop backend services
make logs                 # tail /tmp/mafafd-logs/*.log
make pg-psql              # psql into the mafafd database
make redis-cli            # redis-cli on the local instance
make k6                   # run the load test (needs k6 installed)
make frontend-dev         # Next.js dev server on :3000
make feeder-start         # live market-data feeder (see below)
```

### Running it manually (without make)

```bash
# Postgres (pg_ctl/initdb are often outside $PATH — e.g. /usr/lib/postgresql/18/bin)
/usr/lib/postgresql/18/bin/initdb -D ~/tools/pgdata -U evosynx --auth=trust --no-locale -E UTF8
/usr/lib/postgresql/18/bin/pg_ctl -D ~/tools/pgdata -o "-p 5433 -k /tmp -h 127.0.0.1" -l /tmp/mafafd-logs/postgres.log start
createdb -h 127.0.0.1 -p 5433 -U evosynx mafafd
psql -h 127.0.0.1 -p 5433 -U evosynx -d mafafd -f db/init/001_schema.sql

# Redis
redis-server --port 6379 --appendonly yes

# Backend services (run from the repo root so .env is picked up)
cp .env.example .env
cd backend && for svc in ingestion worker-rules worker-ai gateway; do \
  go build -o bin/$svc ./cmd/$svc; done
setsid nohup ./backend/bin/gateway      > /tmp/mafafd-logs/gateway.log 2>&1 &
setsid nohup ./backend/bin/worker-rules > /tmp/mafafd-logs/worker-rules.log 2>&1 &
setsid nohup ./backend/bin/worker-ai    > /tmp/mafafd-logs/worker-ai.log 2>&1 &
setsid nohup ./backend/bin/ingestion    > /tmp/mafafd-logs/ingestion.log 2>&1 &

# Frontend
cd frontend && npm i && npm run dev
```

---

## Rules engine

Scoring is the **weighted sum of the fired rules' scores, re-normalized over
the active rules** (see `backend/internal/ruleengine`):

| Rule      | Defaults                                                    |
|-----------|-------------------------------------------------------------|
| velocity  | customer 5/600s (0.25), ip 12/60s (0.20), card 3/600s (0.20), device 3/600s (0.10), global 100/min (0.15) |
| geo       | allowlist US/ID/SG, blocklist country +70, unknown +15 (0.15) |
| ip        | blacklist +75 (0.20)                                        |
| amount    | `>= suspicious_from` partial score, `> max_single` +70 (0.20) |
| currency  | mismatch +40 (0.05)                                         |

**Decision bands:** `<30` LOW · `30–70` SUSPICIOUS (queued for AI) · `≥70` HIGH ·
incident created at `≥40`. All thresholds are per-tenant, editable from the
dashboard **Rules** page (stored in `rule_configs`, cached 30s, evicted on save).

---

## Security

- **API keys** stored as SHA-256; shown once; revocable with instant cache
  eviction. HMAC request signing (`X-Mafafd-Signature`, optional).
- **Rate limiting**: per-tenant token bucket in Redis (Lua, atomic).
- **Dashboard auth**: bcrypt login → short-lived HS256 JWT in an **httpOnly,
  SameSite=Strict** cookie; RBAC (`platform`/`admin`/`analyst`).
- **WebSocket**: separate short-lived (`10 min`) token scoped `ws` — an XSS
  that steals it cannot call REST. Issued only from an authenticated session.
- **PII protection**: `customer_id`, `email`, device fingerprint are SHA-256
  hashed at the ingestion edge; card numbers become `card_<hash24>`; IPs are
  masked (`203.0.113.x`) before any LLM egress. The AI prompt never receives
  raw PII.
- **Tenant isolation**: every query is tenant-scoped; platform users can
  override via `X-Tenant-Id`; no cross-tenant leak path in the API surface.
- **Transport & headers**: TLS expected in front of the stack; `X-Frame-Options
  DENY`, `nosniff`, strict Referrer-Policy, restrictive Permissions-Policy.
- **Operational**: non-root containers (uid 1001), JWT length enforced in
  production, structured audit log for every privileged action.

---

## Observability

Prometheus metrics per service (`/metrics`): request volume/latency,
`transactions_{ingested,published,consumed,decision}_total`,
`rule_evaluation_seconds`, `ai_call_duration_seconds`, `ai_fallback_total`,
`queue_depth`, `ws_clients`. A dashboard is auto-provisioned in Grafana.

---

## Stress test (k6)

```bash
k6 run k6/script.js            # 200 VU steady + 800 VU spike (two scenarios)
k6 run k6/steady.js            # steady-only: SLA thresholds (p95 < 20ms)
k6 run k6/spike.js             # spike-only: resilience thresholds (p95 < 200ms)
```

The `< 20 ms` ingestion SLA is asserted on the **steady** scenario
(`ingest_latency_ms p(95)<20`, `http_req_duration p(95)<25`,
`ingest_over_sla rate<0.05`, `ingest_success rate>0.99`). The **spike**
scenario (800 VU, 4x load) asserts resilience: 100% acceptance and bounded
degradation (`http_req_duration p(95)<200`). k6 exits 0 only when every
threshold passes.

Measured on a 4-core dev machine (200 VU steady, ~1.9k tx/s):

| metric | value |
|---|---|
| throughput | ~1,870 tx/s |
| ingest p95 | 9.9 ms |
| ingest p99 | 15.4 ms |
| http p95 | 15.1 ms |
| over-SLA (20 ms) | 0.28 % |
| ingest success | 100 % |

800 VU spike (~3.4k tx/s): 100 % success, http p95 138 ms — graceful
degradation, no failures, no lost messages.

Notes:

- The ingestion API returns **202 Accepted** (async processing); k6 counts
  200/202 as success.
- Per-tenant rate limiting is enforced by `tenants.settings`
  (`rate_limit_per_min`, `burst`; demo tenant defaults to 600/min). Raising it
  for a benchmark:
  `UPDATE tenants SET settings='{"rate_limit_per_min":200000,"burst":400000}' WHERE name='Acme Payments';`
  (the token bucket lives in Redis — `DEL ratelimit:<tenant_id>` to reset).
- The hot path is: in-process API-key auth cache → Redis token bucket → Redis
  Streams publish (2 Redis round trips, 0 DB queries in steady state).

---

## Live data feeder

The default dataset is synthetic (k6 + smoke tests). To stream **real market
data** into the system, `cmd/feeder` pulls live crypto prices (CoinGecko) and
FX rates (ECB reference via Frankfurter) and POSTs realistic transactions to
the ingestion API. Prices/amounts are real-time; customer/card identities are
sample data. About 5% of orders are deliberately large (> $10k) so the rules
engine keeps raising incidents on real market values.

```bash
make feeder                # foreground, one transaction every 5s
make feeder-start          # background (log: /tmp/mafafd-logs/feeder.log)
make feeder-stop           # stop the background feeder
```

Tune it: `./backend/bin/feeder -interval 10 -assets bitcoin,ethereum -once`
(send one batch and exit; `-url`/`-key` default to `http://localhost:8080` and
the demo API key). Watch transactions appear live on the dashboard while the
feeder runs.

---

## Optional: Docker deployment

Docker files are kept for containerized deployment, but **nothing above requires
Docker**. The native run is the supported path.

```bash
cp .env.example .env
docker compose up -d --build
```

This brings up postgres, redis, rabbitmq (use `MQ_PROVIDER=rabbit`), the four Go
services, the frontend, Prometheus (http://localhost:9090) and Grafana
(http://localhost:3001, admin/admin).

---

## Repository layout

```
backend/            Go services (cmd/*) + shared packages (internal/*)
db/init/001_schema.sql   schema (tenants, keys, users, transactions, incidents, rules, audit)
db/ops/             monthly partitioning + archival SQL
frontend/           Next.js 15 dashboard
k6/                 load tests (script.js = steady+SLA spike; steady.js; spike.js)
prometheus/         scrape config                     (optional, Docker only)
grafana/            auto-provisioned datasource + dashboard (optional, Docker only)
docker-compose.yml  full containerized stack          (optional; not needed to run)
Makefile            build/test/run/infra/docker/k6 targets
```

## Operational notes

- Postgres is initialized with `db/init/001_schema.sql`; monthly partitioning
  and archival helpers live in `db/ops/`.
- `SEED_DEMO_DATA` skips seeding if a tenant already exists (not idempotent —
  drop the DB for a clean reseed).
- Worker instances are horizontally scalable (multiple consumers on the same
  queue); the gateway supports multiple replicas behind a load balancer
  (WebSockets are stateless via Redis Pub/Sub).
