import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const API_KEY = __ENV.API_KEY || "sk_live_38f2a91b4c0d5e7f9a1b3c5d7e9f0a2b4c6d8e0f1a3b5c7d9e0f2a4b6c8d";
const SLOW_MS = 20;
const params = { headers: { "Content-Type": "application/json", "X-Api-Key": API_KEY } };
const BASE_PAYLOADS = Array.from({ length: 20 }, (_, i) => ({
  external_id: `steady-${i}`,
  amount: 50 + Math.floor(Math.random() * 5000),
  currency: "USD",
  customer_id: `cust-${i}`,
  customer_email: `cust${i}@example.com`,
  card_token: `tok_${i}`,
  ip_address: `203.0.113.${i}`,
  country_code: "US",
  city: "New York",
}));
function makePayload() { return JSON.stringify(BASE_PAYLOADS[__ITER % BASE_PAYLOADS.length]); }

const okRate = new Rate("ingest_success");
const latency = new Trend("ingest_latency_ms");
const slowRate = new Rate("ingest_over_sla");

export default function () {
  const res = http.post(`${BASE_URL}/api/v1/transactions`, makePayload(), params);
  const ok = res.status === 200 || res.status === 202;
  okRate.add(ok);
  let ms = -1;
  if (ok) { const data = res.json(); ms = data.estimated_processing_ms ?? -1; }
  latency.add(ms);
  slowRate.add(ms >= 0 && ms > SLOW_MS);
  check(res, { "status is 200/202": (r) => r.status === 200 || r.status === 202 });
  sleep(0.1);
}

export const options = {
  vus: 200,
  duration: "3m",
  thresholds: {
    ingest_success: ["rate>=0.99"],
    http_req_duration: ["p(95)<20"],
    ingest_latency_ms: ["p(95)<20"],
    ingest_over_sla: ["rate<0.05"],
  },
};
