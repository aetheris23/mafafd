"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, Search } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Drawer } from "@/components/ui/drawer";
import { Input, Select } from "@/components/ui/input";
import { Spinner } from "@/components/ui/loading";
import { Table, TBody, Td, Th, THead } from "@/components/ui/table";
import type { Transaction } from "@/lib/types";
import { cn, fetchJSON, formatCurrency, formatDateTime, riskColor } from "@/lib/utils";

const STATUSES = ["processed", "flagged", "investigated", "resolved", "refunded"];

export default function TransactionsPage() {
  const [items, setItems] = useState<Transaction[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [perPage] = useState(25);
  const [search, setSearch] = useState("");
  const [level, setLevel] = useState("");
  const [status, setStatus] = useState("");
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Transaction | null>(null);
  const [busyId, setBusyId] = useState("");
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const load = useCallback(async () => {
    setLoading(true);
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) });
    if (search) params.set("search", search);
    if (level) params.set("level", level);
    if (status) params.set("status", status);
    try {
      const data = await fetchJSON<{ items: Transaction[]; total: number }>(`/api/v1/transactions?${params}`);
      setItems(data.items);
      setTotal(data.total);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [page, perPage, search, level, status]);

  useEffect(() => {
    load();
  }, [load]);

  const onSearch = (v: string) => {
    setSearch(v);
    debounceRef.current = setTimeout(() => setPage(1), 350);
  };

  const updateStatus = async (id: string, next: string) => {
    setBusyId(id);
    try {
      await fetchJSON<{ ok: boolean }>(`/api/v1/transactions/${id}`, {
        method: "PATCH",
        body: JSON.stringify({ status: next }),
      });
      await load();
    } finally {
      setBusyId("");
    }
  };

  const pages = Math.max(1, Math.ceil(total / perPage));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">Transactions</h1>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search size={14} className="absolute left-2.5 top-2.5 text-muted-foreground" />
            <Input
              placeholder="Search ext id, card, country…"
              value={search}
              onChange={(e) => onSearch(e.target.value)}
              className="w-64 pl-8"
            />
          </div>
          <Select value={level} onChange={(e) => { setLevel(e.target.value); setPage(1); }} className="w-36">
            <option value="">All risk</option>
            <option value="LOW">Low</option>
            <option value="SUSPICIOUS">Suspicious</option>
            <option value="HIGH">High</option>
          </Select>
          <Select value={status} onChange={(e) => { setStatus(e.target.value); setPage(1); }} className="w-36">
            <option value="">All status</option>
            {STATUSES.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </Select>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <THead>
              <Th>Time</Th>
              <Th>External ID</Th>
              <Th>Amount</Th>
              <Th>Card</Th>
              <Th>Country</Th>
              <Th>Risk</Th>
              <Th>AI</Th>
              <Th>Status</Th>
            </THead>
            <TBody>
              {loading && (
                <tr>
                  <Td colSpan={8} className="py-8 text-center text-muted-foreground"><Spinner className="mx-auto" /></Td>
                </tr>
              )}
              {!loading && items.length === 0 && (
                <tr>
                  <Td colSpan={8} className="py-8 text-center text-muted-foreground">No transactions found.</Td>
                </tr>
              )}
              {items.map((t) => (
                <tr key={t.id} className="cursor-pointer transition-colors hover:bg-accent/50" onClick={() => setSelected(t)}>
                  <Td className="whitespace-nowrap text-muted-foreground">{formatDateTime(t.transaction_time)}</Td>
                  <Td className="font-mono text-xs">{t.external_id || short(t.id)}</Td>
                  <Td className="font-medium">{formatCurrency(t.amount, t.currency)}</Td>
                  <Td className="font-mono text-xs text-muted-foreground">{t.card_token}</Td>
                  <Td>{t.country_code || "-"}</Td>
                  <Td>
                    <div className="flex items-center gap-2">
                      <Badge className={riskColor(t.risk_level)}>{t.risk_level}</Badge>
                      <span className="text-xs text-muted-foreground">{t.risk_score.toFixed(1)}</span>
                    </div>
                  </Td>
                  <Td>{t.ai_verdict ? <Badge className="border-border text-muted-foreground">{t.ai_verdict}</Badge> : <span className="text-muted-foreground">-</span>}</Td>
                  <Td>
                    <div className="flex items-center gap-1">
                      <Badge className="border-border text-muted-foreground">{t.status}</Badge>
                      {busyId === t.id && <Spinner className="h-3 w-3" />}
                    </div>
                  </Td>
                </tr>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{total} transactions</span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            <ChevronLeft size={14} /> Prev
          </Button>
          <span className="px-1">Page {page} of {pages}</span>
          <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>
            Next <ChevronRight size={14} />
          </Button>
        </div>
      </div>

      <Drawer open={!!selected} onClose={() => setSelected(null)} title={selected ? `Transaction ${selected.external_id || short(selected.id)}` : ""}>
        {selected && (
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-3 text-sm">
              <Info k="Amount" v={`${formatCurrency(selected.amount, selected.currency)}`} />
              <Info k="Risk score" v={`${selected.risk_score.toFixed(1)} (${selected.risk_level})`} />
              <Info k="Deterministic" v={selected.deterministic_score.toFixed(1)} />
              <Info k="AI verdict" v={selected.ai_verdict || "—"} />
              <Info k="AI score" v={selected.ai_score != null ? selected.ai_score.toFixed(1) : "—"} />
              <Info k="Country / city" v={`${selected.country_code || "-"} / ${selected.city || "-"}`} />
              <Info k="Card" v={selected.card_token} mono />
              <Info k="IP" v={selected.ip_address || "-"} mono />
              <Info k="Merchant category" v={selected.merchant_category || "-"} />
              <Info k="Processed at" v={formatDateTime(selected.processed_at)} />
            </div>

            {selected.rule_breakdown && selected.rule_breakdown.length > 0 && (
              <div>
                <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Rule breakdown</h4>
                <div className="space-y-1.5">
                  {selected.rule_breakdown.map((r) => (
                    <div key={r.rule} className="flex items-center justify-between rounded-md border border-border/50 px-3 py-1.5 text-xs">
                      <div>
                        <span className="font-medium">{r.rule}</span>
                        <p className="text-muted-foreground">{r.detail}</p>
                      </div>
                      <span className="font-semibold">{r.score.toFixed(1)}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {selected.ai_explanation && (
              <div>
                <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">AI explanation</h4>
                <p className="rounded-md border border-border/50 bg-muted/40 p-3 text-xs leading-relaxed text-foreground/90">
                  {selected.ai_explanation}
                </p>
              </div>
            )}

            <div>
              <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">Update status</h4>
              <div className="flex flex-wrap gap-1.5">
                {STATUSES.map((s) => (
                  <Button
                    key={s}
                    size="sm"
                    variant={selected.status === s ? "primary" : "outline"}
                    onClick={() => updateStatus(selected.id, s)}
                  >
                    {s}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}

function Info({ k, v, mono }: { k: string; v: string; mono?: boolean }) {
  return (
    <div className="rounded-md border border-border/50 px-3 py-2">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{k}</div>
      <div className={cn("mt-0.5 text-sm font-medium", mono && "font-mono text-xs")}>{v}</div>
    </div>
  );
}

function short(id: string) {
  return id.slice(0, 8);
}
