import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatCurrency(amount: number, currency: string = "USD") {
  try {
    return new Intl.NumberFormat("en-US", { style: "currency", currency }).format(amount);
  } catch {
    return `$${amount.toFixed(2)}`;
  }
}

export function formatNumber(n: number) {
  return new Intl.NumberFormat("en-US").format(n);
}

export function formatCompact(n: number) {
  return new Intl.NumberFormat("en-US", { notation: "compact", maximumFractionDigits: 1 }).format(n);
}

export function formatDateTime(iso: string | undefined) {
  if (!iso) return "-";
  try {
    return new Date(iso).toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  } catch {
    return iso;
  }
}

export function formatTime(iso: string | undefined) {
  if (!iso) return "-";
  try {
    return new Date(iso).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  } catch {
    return iso;
  }
}

export function riskColor(level: string) {
  switch (level) {
    case "HIGH":
      return "text-red-400 border-red-500/40 bg-red-500/10";
    case "SUSPICIOUS":
      return "text-amber-400 border-amber-500/40 bg-amber-500/10";
    default:
      return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
  }
}

export function incidentColor(status: string) {
  switch (status) {
    case "open":
      return "text-red-400 border-red-500/40 bg-red-500/10";
    case "investigating":
      return "text-amber-400 border-amber-500/40 bg-amber-500/10";
    case "resolved":
      return "text-emerald-400 border-emerald-500/40 bg-emerald-500/10";
    case "false_positive":
      return "text-sky-400 border-sky-500/40 bg-sky-500/10";
    default:
      return "text-muted-foreground border-border bg-muted";
  }
}

export function shortId(id: string) {
  return id ? id.slice(0, 8) : "-";
}

export async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    cache: "no-store",
    ...init,
    headers: { "content-type": "application/json", ...(init?.headers || {}) },
  });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error((data && (data as { error?: string }).error) || `Request failed (${res.status})`);
  }
  return data as T;
}
