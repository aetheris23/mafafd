"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import {
  Activity,
  AlertTriangle,
  ArrowDownRight,
  ArrowUpRight,
  DollarSign,
  ShieldCheck,
  Timer,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/loading";
import { useLiveEvents, type LiveEvent } from "@/components/LiveProvider";
import type { Bucket, Incident, Overview } from "@/lib/types";
import { fetchJSON, formatCompact, formatCurrency, formatNumber, formatTime, incidentColor, riskColor } from "@/lib/utils";

const PIE_COLORS = ["#22c55e", "#f59e0b", "#ef4444"];

export default function OverviewPage() {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [series, setSeries] = useState<Bucket[]>([]);
  const [distribution, setDistribution] = useState<{ level: string; value: number }[]>([]);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [liveFeed, setLiveFeed] = useState<LiveEvent[]>([]);
  const feedRef = useRef<LiveEvent[]>([]);

  const router = useRouter();
  const live = useLiveEvents();

  useEffect(() => {
    fetchJSON<Overview>("/api/v1/stats/overview").then(setOverview).catch(() => {});
    fetchJSON<Bucket[]>("/api/v1/stats/timeseries?hours=1")
      .then(setSeries)
      .catch(() => {});
    fetchJSON<Record<string, number>>("/api/v1/stats/risk-distribution")
      .then((m) => {
        const arr = Object.entries(m)
          .map(([level, value]) => ({ level, value }))
          .filter((d) => d.value > 0)
          .sort((a, b) => b.value - a.value);
        setDistribution(arr);
      })
      .catch(() => {});
    fetchJSON<{ items: Incident[] }>("/api/v1/incidents?limit=6")
      .then((d) => setIncidents(d.items))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (live.length === 0) return;
    feedRef.current = [...live.slice().reverse(), ...feedRef.current].slice(0, 12);
    setLiveFeed([...feedRef.current]);
    for (const ev of live) {
      if (ev.event === "transaction.processed" && ev.data) {
        setOverview((o) =>
          o
            ? {
                ...o,
                tx_total: o.tx_total + 1,
                tx_last_minute: o.tx_last_minute + 1,
                tx_last_hour: o.tx_last_hour + 1,
                anomaly_count: o.anomaly_count + (ev.data?.is_anomaly ? 1 : 0),
                last_processed: ev.at,
              }
            : o,
        );
      }
      if (ev.event === "incident.flagged" && ev.data) {
        setOverview((o) => (o ? { ...o, open_incidents: o.open_incidents + 1 } : o));
        setIncidents((prev) => [ev.data, ...prev].slice(0, 6));
      }
      if (ev.event === "ai.updated") {
        setOverview((o) => (o ? { ...o, ai_count: o.ai_count + 1 } : o));
      }
    }
  }, [live]);

  const onIncidentClick = useCallback(
    (incidentId: string) => router.push(`/incidents?focus=${incidentId}`),
    [router],
  );

  if (!overview) {
    return (
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Card key={i}>
            <CardContent className="space-y-3 p-4">
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-8 w-32" />
            </CardContent>
          </Card>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="font-mono text-base font-semibold uppercase tracking-wider">Overview</h1>
        {overview.last_processed && (
          <span className="text-xs text-muted-foreground">Last processed {formatTime(overview.last_processed)}</span>
        )}
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          icon={<Activity size={18} />}
          label="Transactions (24h)"
          value={formatNumber(overview.tx_total)}
          sub={`${formatCompact(overview.tx_last_hour)} last hour`}
        />
        <StatCard
          icon={<AlertTriangle size={18} />}
          label="Anomalies Detected"
          value={formatNumber(overview.anomaly_count)}
          sub={`${formatNumber(overview.high_risk_count)} high risk`}
          tone="amber"
        />
        <StatCard
          icon={<ShieldCheck size={18} />}
          label="Open Incidents"
          value={formatNumber(overview.open_incidents)}
          sub="needs review"
          tone="red"
        />
        <StatCard
          icon={<Timer size={18} />}
          label="Avg Risk Score"
          value={overview.avg_risk.toFixed(1)}
          sub={`${formatNumber(overview.ai_count)} AI-assessed`}
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Transaction volume & anomalies — last hour</CardTitle>
            <Badge className="border-border text-muted-foreground">live</Badge>
          </CardHeader>
          <CardContent>
            <div className="h-64">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={series} margin={{ top: 8, right: 8, left: -16, bottom: 0 }}>
                  <defs>
                    <linearGradient id="gCount" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#22c55e" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="#22c55e" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="gAnom" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#ef4444" stopOpacity={0.45} />
                      <stop offset="100%" stopColor="#ef4444" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="hsl(155 12% 16%)" />
                  <XAxis dataKey="ts" stroke="hsl(150 12% 50%)" fontSize={10} tickFormatter={(v) => v} />
                  <YAxis stroke="hsl(150 12% 50%)" fontSize={10} allowDecimals={false} />
                  <Tooltip
                    contentStyle={{ background: "hsl(160 15% 9%)", border: "1px solid hsl(155 12% 18%)", borderRadius: 6 }}
                    labelStyle={{ color: "hsl(150 15% 80%)" }}
                  />
                  <Area type="monotone" dataKey="count" name="Transactions" stroke="#22c55e" fill="url(#gCount)" strokeWidth={2} />
                  <Area type="monotone" dataKey="anomalies" name="Anomalies" stroke="#ef4444" fill="url(#gAnom)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Risk distribution</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-40">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie data={distribution} dataKey="value" nameKey="level" innerRadius={45} outerRadius={70} paddingAngle={3}>
                    {distribution.map((entry, idx) => (
                      <Cell key={entry.level} fill={PIE_COLORS[idx % PIE_COLORS.length]} />
                    ))}
                  </Pie>
                  <Tooltip
                    contentStyle={{ background: "hsl(160 15% 9%)", border: "1px solid hsl(155 12% 18%)", borderRadius: 6 }}
                  />
                </PieChart>
              </ResponsiveContainer>
            </div>
            <div className="mt-2 space-y-1.5">
              {distribution.map((d, idx) => (
                <div key={d.level} className="flex items-center justify-between text-xs">
                  <span className="flex items-center gap-2 text-muted-foreground">
                    <span className="h-2 w-2 rounded-full" style={{ background: PIE_COLORS[idx % PIE_COLORS.length] }} />
                    {d.level}
                  </span>
                  <span className="font-medium">{formatNumber(d.value)}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Recent incidents</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {incidents.length === 0 && <p className="py-6 text-center text-sm text-muted-foreground">No incidents yet.</p>}
            {incidents.map((inc) => (
              <button
                key={inc.id}
                onClick={() => onIncidentClick(inc.id)}
                className="flex w-full items-center justify-between gap-3 rounded-md border border-border/50 px-3 py-2 text-left transition-colors hover:bg-accent"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Badge className={riskColor(inc.risk_level)}>{inc.risk_level}</Badge>
                    <span className="truncate text-xs text-muted-foreground">{inc.reason}</span>
                  </div>
                </div>
                <div className="shrink-0 text-right">
                  <div className="text-sm font-semibold">{inc.risk_score.toFixed(1)}</div>
                  <div className="text-[10px] uppercase text-muted-foreground">{formatTime(inc.created_at)}</div>
                </div>
              </button>
            ))}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Live event stream</CardTitle>
            <Badge className="border-emerald-500/40 bg-emerald-500/10 text-emerald-400">streaming</Badge>
          </CardHeader>
          <CardContent className="space-y-1.5">
            {liveFeed.length === 0 && (
              <p className="py-6 text-center text-sm text-muted-foreground">
                Waiting for events… send a transaction to <code className="rounded bg-muted px-1">POST /api/v1/transactions</code>
              </p>
            )}
            {liveFeed.map((ev) => (
              <div key={ev.seq} className="flex items-center gap-2.5 rounded-md border border-border/40 px-3 py-1.5 text-xs">
                <span
                  className={`inline-block h-1.5 w-1.5 shrink-0 rounded-full ${
                    ev.event === "incident.flagged" ? "bg-red-500" : "bg-emerald-400"
                  }`}
                />
                <span className="font-mono text-muted-foreground">{formatTime(ev.at)}</span>
                <span className="text-foreground">{ev.event}</span>
                {ev.event === "incident.flagged" && ev.data && (
                  <span className="ml-auto font-medium text-red-400">risk {ev.data.risk_score?.toFixed?.(1)}</span>
                )}
                {ev.event === "transaction.processed" && ev.data && (
                  <span className="ml-auto flex items-center gap-1 font-medium text-muted-foreground">
                    {formatCurrency(ev.data.amount, ev.data.currency)}
                    {ev.data.is_anomaly && <ArrowUpRight size={12} className="text-amber-400" />}
                  </span>
                )}
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function StatCard({
  icon,
  label,
  value,
  sub,
  tone = "default",
}: {
  icon: React.ReactNode;
  label: string;
  value: string;
  sub: string;
  tone?: "default" | "amber" | "red";
}) {
  const toneIcon =
    tone === "red" ? "text-red-400 bg-red-500/10" : tone === "amber" ? "text-amber-400 bg-amber-500/10" : "text-primary bg-primary/10";
  const toneValue = tone === "red" ? "text-red-400" : tone === "amber" ? "text-amber-400" : "";
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-center gap-3">
          <div className={`flex h-9 w-9 items-center justify-center rounded-md ${toneIcon}`}>{icon}</div>
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className={`text-xl font-semibold tracking-tight ${toneValue}`}>{value}</div>
          </div>
        </div>
        <div className="mt-2 flex items-center gap-1 text-[11px] text-muted-foreground">{sub}</div>
      </CardContent>
    </Card>
  );
}
