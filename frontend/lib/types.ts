export type RiskLevel = "LOW" | "SUSPICIOUS" | "HIGH";
export type UserRole = "platform" | "admin" | "analyst";

export interface User {
  id: string;
  email: string;
  role: UserRole;
  tenant: { id: string; name: string };
}

export interface RuleScore {
  rule: string;
  score: number;
  weight: number;
  detail: string;
}

export interface Transaction {
  id: string;
  tenant_id: string;
  external_id: string;
  amount: number;
  currency: string;
  customer_id_hash: string;
  customer_email_hash: string;
  card_token: string;
  ip_address: string;
  device_fingerprint_hash: string;
  country_code: string;
  city: string;
  merchant_category: string;
  merchant_name: string;
  payment_method: string;
  risk_score: number;
  risk_level: RiskLevel;
  deterministic_score: number;
  ai_score?: number;
  ai_verdict?: string;
  ai_explanation?: string;
  rule_breakdown: RuleScore[];
  is_anomaly: boolean;
  status: string;
  transaction_time: string;
  created_at: string;
  processed_at?: string;
}

export interface Incident {
  id: string;
  tenant_id: string;
  transaction_id: string;
  risk_score: number;
  risk_level: RiskLevel;
  reason: string;
  ai_explanation?: string;
  status: "open" | "investigating" | "resolved" | "false_positive";
  note?: string;
  created_at: string;
  updated_at: string;
  transaction?: Transaction;
}

export interface Overview {
  tx_total: number;
  tx_last_minute: number;
  tx_last_hour: number;
  anomaly_count: number;
  open_incidents: number;
  avg_risk: number;
  high_risk_count: number;
  ai_count: number;
  last_processed: string;
}

export interface Bucket {
  ts: string;
  count: number;
  anomalies: number;
  avg_risk: number;
  sum_amount: number;
}

export interface APIKey {
  id: string;
  name: string;
  key_prefix: string;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
}

export interface APIKeyCreated extends APIKey {
  key: string;
  show_once: boolean;
}

export interface WSEvent {
  event: "transaction.processed" | "incident.flagged" | "ai.updated" | "ping";
  at: string;
  data?: any;
}
