# =============================================================================
# MAFAFD — Multi-Tenant AI Financial Anomaly & Fraud Detector
#
# Native run (no Docker). Requires: Go 1.26+, Node 24+, Redis, PostgreSQL.
#   make infra-start   # start local Redis + PostgreSQL (see variables below)
#   make run           # build + start the four backend services
#   make frontend-dev  # Next.js dev server on :3000
#   make stop          # stop the backend services
# =============================================================================

SHELL := /bin/bash
BACKEND := backend
FRONTEND := frontend
BIN     := $(BACKEND)/bin
LOGS    := /tmp/mafafd-logs

# --- local infrastructure (override via env if your tools live elsewhere) ---
PGDATA ?= $(HOME)/tools/pgdata
PGBIN  ?= /usr/lib/postgresql/18/bin
PGUSER ?= evosynx
PGPASSWORD ?= evosynx01
PGPORT ?= 5433
PGHOST ?= 127.0.0.1
REDIS_SERVER ?= $(HOME)/tools/redis-7.4.2/src/redis-server
REDIS_CLI ?= $(HOME)/tools/redis-7.4.2/src/redis-cli
REDIS_PORT ?= 6379

# ---------------------------------------------------------------- build ------
.PHONY: all backend frontend
all: backend frontend

backend:
	@echo ">> building backend binaries"
	@cd $(BACKEND) && for svc in ingestion worker-rules worker-ai gateway feeder; do \
		go build -trimpath -ldflags="-s -w" -o bin/$$svc ./cmd/$$svc || exit 1; \
	done
	@echo ">> done: $(BIN)/{ingestion,worker-rules,worker-ai,gateway,feeder}"

frontend:
	@echo ">> building frontend"
	@cd $(FRONTEND) && npm ci --no-audit --no-fund && npm run build

# ---------------------------------------------------------------- verify -----
.PHONY: test lint vet tidy
test:
	@cd $(BACKEND) && go test ./...

vet:
	@cd $(BACKEND) && go vet ./...

lint: vet
	@cd $(FRONTEND) && npx tsc --noEmit

tidy:
	@cd $(BACKEND) && go mod tidy

# ------------------------------------------- local infrastructure (native) ---
# PostgreSQL: own cluster so the system install stays untouched.
.PHONY: pg-init pg-start pg-stop pg-psql
pg-init:
	@test -d $(PGDATA) || $(PGBIN)/initdb -D $(PGDATA) -U $(PGUSER) --auth=trust --no-locale -E UTF8
	@echo ">> cluster ready at $(PGDATA) (superuser $(PGUSER))"

pg-start:
	@mkdir -p $(LOGS)
	@$(PGBIN)/pg_ctl -D $(PGDATA) -o "-p $(PGPORT) -k /tmp -h $(PGHOST)" -l $(LOGS)/postgres.log start || true
	@sleep 1
	@psql -h $(PGHOST) -p $(PGPORT) -U $(PGUSER) -d postgres -c \
		"ALTER ROLE $(PGUSER) LOGIN PASSWORD '$(PGPASSWORD)';" 2>/dev/null || true
	@createdb -h $(PGHOST) -p $(PGPORT) -U $(PGUSER) mafafd 2>/dev/null || true
	@psql -h $(PGHOST) -p $(PGPORT) -U $(PGUSER) -d mafafd -f db/init/001_schema.sql
	@echo ">> postgres up on $(PGHOST):$(PGPORT) (db=mafafd, user=$(PGUSER))"

pg-stop:
	@$(PGBIN)/pg_ctl -D $(PGDATA) stop -m fast || true

pg-psql:
	@psql -h $(PGHOST) -p $(PGPORT) -U $(PGUSER) -d mafafd

# Redis: start daemonized with AOF persistence on.
.PHONY: redis-start redis-stop redis-cli
redis-start:
	@$(REDIS_SERVER) --port $(REDIS_PORT) --appendonly yes --daemonize yes --pidfile /tmp/mafafd-redis.pid || true
	@$(REDIS_CLI) -p $(REDIS_PORT) ping

redis-stop:
	@$(REDIS_CLI) -p $(REDIS_PORT) shutdown nosave 2>/dev/null || true

redis-cli:
	@$(REDIS_CLI) -p $(REDIS_PORT)

.PHONY: infra-start infra-stop
infra-start: pg-start redis-start
	@echo ">> infra up (postgres :$(PGPORT), redis :$(REDIS_PORT))"

infra-stop: redis-stop pg-stop
	@echo ">> infra stopped"

# ---------------------------------------------------------- services run ----
.PHONY: run run-ingestion run-rules run-ai run-gateway stop logs
run: backend
	@mkdir -p $(LOGS)
	@setsid nohup $(BIN)/gateway       > $(LOGS)/gateway.log 2>&1 &
	@setsid nohup $(BIN)/worker-rules  > $(LOGS)/worker-rules.log 2>&1 &
	@setsid nohup $(BIN)/worker-ai     > $(LOGS)/worker-ai.log 2>&1 &
	@setsid nohup $(BIN)/ingestion     > $(LOGS)/ingestion.log 2>&1 &
	@sleep 1
	@echo ">> services started (logs in $(LOGS))"
	@echo ">> health:   curl http://localhost:8080/health  (ingestion)"
	@echo ">>           curl http://localhost:8081/health  (gateway)"
	@echo ">> dashboard: http://localhost:3000  (start frontend separately)"

stop:
	@pkill -x ingestion 2>/dev/null; \
	pkill -x gateway 2>/dev/null; \
	pkill -x worker-rules 2>/dev/null; \
	pkill -x worker-ai 2>/dev/null; \
	pkill -x feeder 2>/dev/null; \
	true
	@echo ">> services stopped"

logs:
	@tail -f $(LOGS)/*.log

# ----------------------------------------------------------------- feeder -----
# Streams REAL market data (CoinGecko prices + ECB/BI-derived FX) into the
# ingestion API. Amounts/prices are live; parties are synthetic.
.PHONY: feeder feeder-start feeder-stop
feeder: backend
	@$(BIN)/feeder -interval 5

feeder-start: backend
	@mkdir -p $(LOGS)
	@setsid nohup $(BIN)/feeder -interval 5 > $(LOGS)/feeder.log 2>&1 &
	@sleep 1
	@echo ">> feeder started (see $(LOGS)/feeder.log); stop with 'make feeder-stop'"

feeder-stop:
	@pkill -x feeder 2>/dev/null; true
	@echo ">> feeder stopped"

# ---------------------------------------------------------------- frontend ----
.PHONY: frontend-dev frontend-build
frontend-dev:
	@cd $(FRONTEND) && npm i --no-audit --no-fund && npm run dev

frontend-build: frontend

.PHONY: frontend-stop
frontend-stop:
	@pkill -f "next start" 2>/dev/null; pkill -f "next dev" 2>/dev/null; true

# ------------------------------------------------------------------ k6 ------
.PHONY: k6
k6:
	@k6 run k6/script.js

# ------------------------------------------------------------ docker (optional)
# Only needed for containerized deployment; the native run above never touches
# Docker.
.PHONY: up down up-build ps
up:
	docker compose up -d

up-build:
	docker compose up -d --build

down:
	docker compose down

ps:
	docker compose ps

# ---------------------------------------------------------------- clean ------
.PHONY: clean
clean:
	@rm -rf $(BIN)
	@cd $(FRONTEND) && rm -rf .next
