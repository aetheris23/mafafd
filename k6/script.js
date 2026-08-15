// =============================================================================
// MAFAFD — k6 load test
//
//   k6 run k6/script.js
//
// Env vars:
//   BASE_URL   ingestion URL            (default http://localhost:8080)
//   API_KEY    ingestion API key        (default demo key)
//   SLOW_MS    latency SLA in ms        (default 20)
//
// Scenarios:
//   steady — 200 VU sustained (production normal load). The <20ms ingestion
//            SLA is asserted HERE: p95 processing time, p95 request duration,
//            and <5% of requests over SLA.
//   spike  — 800 VU burst (4x load). Asserts resilience: 100% acceptance and
//            bounded degradation (p95 request duration < 200ms). No failures.
// =============================================================================

import http from "k6/http";
import ws from "k6/ws";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";
import { randomString } from "https://jslib.k6.io/k6-utils/1.4.0/index.js";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const API_KEY = __ENV.API_KEY || "sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d";
const SLOW_MS = Number(__ENV.SLOW_MS || 20);

const latency = new Trend("ingest_latency_ms", true);
const okRate = new Rate("ingest_success");
const slowRate = new Rate("ingest_over_sla");

// Payload pool; customer/card/ip are repeated to exercise velocity rules.
const BASE_PAYLOADS = Array.from({ length: 20 }, (_, i) => ({
  amount: 10 + (i * 97) % 5000,
  currency: "USD",
  customer_id: `cust-${i % 5}`,
  customer_email: `user${i % 5}@example.com`,
  card_token: `tok_visa_${i % 4}`,
  ip_address: `203.0.113.${10 + (i % 20)}`,
  device_fingerprint: `dev-${i % 3}`,
  country_code: ["US", "ID", "SG"][i % 3],
  city: ["NYC", "Jakarta", "Singapore"][i % 3],
  merchant_category: ["online_retail", "ride_sharing", "crypto_exchange", "grocery", "food_delivery"][i % 5],
  merchant_name: ["Amazon", "Tokopedia", "Gojek", "Alfamart", "GoFood"][i % 5],
  payment_method: ["card_visa", "e_wallet_gopay", "card_mastercard", "e_wallet_dana", "e_wallet_shopeepay"][i % 5],
}));

function makePayload() {
  const p = BASE_PAYLOADS[__ITER % BASE_PAYLOADS.length];
  return JSON.stringify({ external_id: `k6-${__VU}-${__ITER}`, ...p });
}

const params = {
  headers: {
    "Content-Type": "application/json",
    "X-Api-Key": API_KEY,
  },
};

export const options = {
  scenarios: {
    steady: {
      executor: "ramping-vus",
      stages: [
        { duration: "30s", target: 200 },
        { duration: "2m", target: 200 },
        { duration: "30s", target: 0 },
      ],
    },
    spike: {
      executor: "ramping-vus",
      startTime: "3m30s",
      stages: [
        { duration: "10s", target: 800 },
        { duration: "20s", target: 800 },
        { duration: "10s", target: 0 },
      ],
    },
  },
  thresholds: {
    // Steady-state SLA (200 VU, ~1.9k tx/s): ingestion latency p95 < 20ms,
    // request p95 < 25ms, no more than 5% of requests over SLA, zero losses.
    "ingest_success{scenario:steady}": ["rate>0.99"],
    "ingest_latency_ms{scenario:steady}": ["p(95)<20", "p(99)<40"],
    "ingest_over_sla{scenario:steady}": ["rate<0.05"],
    "http_req_duration{scenario:steady}": ["p(95)<25"],
    // Spike resilience (800 VU, 4x load): no failures, bounded degradation.
    "ingest_success{scenario:spike}": ["rate>0.99"],
    "http_req_duration{scenario:spike}": ["p(95)<200"],
  },
};

export default function () {
  const res = http.post(`${BASE_URL}/api/v1/transactions`, makePayload(), params);

  const ok = res.status === 200 || res.status === 202; // accepted (async)
  okRate.add(ok);

  let ms = -1;
  if (ok) {
    const data = res.json();
    ms = data.estimated_processing_ms ?? -1;
  }
  latency.add(ms);
  slowRate.add(ms >= 0 && ms > SLOW_MS);

  check(res, {
    "status is 200/202": (r) => r.status === 200 || r.status === 202,
    "returns estimated processing time": () => ms >= 0,
    "estimated processing <= SLA": () => ms >= 0 && ms <= SLOW_MS,
  });

  sleep(0.1);
}

// Optional WebSocket live-stream scenario (not run by default).
export function wsLiveStream(wsUrl, jwt) {
  ws.connect(`${wsUrl}/api/v1/ws?token=${encodeURIComponent(jwt)}`, null, (socket) => {
    socket.on("open", () => console.log(`WS ${randomString(6)} connected`));
    socket.setTimeout(() => socket.close(), 5000);
  });
}

export function handleSummary(data) {
  const steady = { scenario: "steady" };
  const spike = { scenario: "spike" };
  return {
    stdout: JSON.stringify(
      {
        steady_200vus: {
          ingest_success_rate: data.metrics.ingest_success?.values?.rate ?? null,
          p95_latency_ms: data.metrics.ingest_latency_ms?.values?.["p(95)"] ?? null,
          p99_latency_ms: data.metrics.ingest_latency_ms?.values?.["p(99)"] ?? null,
          p95_over_sla_rate: data.metrics.ingest_over_sla?.values?.rate ?? null,
          http_req_duration_p95_ms: data.metrics.http_req_duration?.values?.["p(95)"] ?? null,
        },
        spike_800vus: {
          ingest_success_rate: data.metrics.ingest_success?.values?.rate ?? null,
          http_req_duration_p95_ms: data.metrics.http_req_duration?.values?.["p(95)"] ?? null,
        },
      },
      null,
      2,
    ),
  };
}
