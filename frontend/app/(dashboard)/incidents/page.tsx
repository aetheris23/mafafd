"use client";

import { useCallback, useEffect, useState } from "react";
import { ChevronLeft, ChevronRight, Sparkles } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Drawer } from "@/components/ui/drawer";
import { Select } from "@/components/ui/input";
import { Spinner } from "@/components/ui/loading";
import { Table, TBody, Td, Th, THead } from "@/components/ui/table";
import type { Incident } from "@/lib/types";
import { fetchJSON, formatDateTime, incidentColor, riskColor } from "@/lib/utils";

const STATUSES = ["open", "investigating", "resolved", "false_positive"];

export default function IncidentsPage() {
  const [items, setItems] = useState<Incident[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [perPage] = useState(25);
  const [status, setStatus] = useState("");
  const [level, setLevel] = useState("");
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Incident | null>(null);
  const [busyId, setBusyId] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) });
    if (status) params.set("status", status);
    if (level) params.set("level", level);
    try {
      const data = await fetchJSON<{ items: Incident[]; total: number }>(`/api/v1/incidents?${params}`);
      setItems(data.items);
      setTotal(data.total);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [page, perPage, status, level]);

  useEffect(() => {
    load();
  }, [load]);

  // Deep-link support: /incidents?focus=<id>
  useEffect(() => {
    const focus = new URLSearchParams(window.location.search).get("focus");
    if (focus && !selected) {
      const found = items.find((i) => i.id === focus);
      if (found) setSelected(found);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items]);

  const updateStatus = async (id: string, next: string, note?: string) => {
    setBusyId(id);
    try {
      await fetchJSON<{ ok: boolean }>(`/api/v1/incidents/${id}`, {
        method: "PATCH",
        body: JSON.stringify({ status: next, note }),
      });
      await load();
      if (selected?.id === id) setSelected({ ...selected, status: next as Incident["status"] });
    } finally {
      setBusyId("");
    }
  };

  const pages = Math.max(1, Math.ceil(total / perPage));

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">Incidents</h1>
        <div className="flex items-center gap-2">
          <Select value={level} onChange={(e) => { setLevel(e.target.value); setPage(1); }} className="w-36">
            <option value="">All risk</option>
            <option value="LOW">Low</option>
            <option value="SUSPICIOUS">Suspicious</option>
            <option value="HIGH">High</option>
          </Select>
          <Select value={status} onChange={(e) => { setStatus(e.target.value); setPage(1); }} className="w-40">
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
              <Th>Created</Th>
              <Th>Risk</Th>
              <Th>Reason</Th>
              <Th>AI Assessment</Th>
              <Th>Status</Th>
              <Th>Updated</Th>
            </THead>
            <TBody>
              {loading && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center text-muted-foreground"><Spinner className="mx-auto" /></Td>
                </tr>
              )}
              {!loading && items.length === 0 && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center text-muted-foreground">No incidents found.</Td>
                </tr>
              )}
              {items.map((inc) => (
                <tr key={inc.id} className="cursor-pointer transition-colors hover:bg-accent/50" onClick={() => setSelected(inc)}>
                  <Td className="whitespace-nowrap text-muted-foreground">{formatDateTime(inc.created_at)}</Td>
                  <Td>
                    <div className="flex items-center gap-2">
                      <Badge className={riskColor(inc.risk_level)}>{inc.risk_level}</Badge>
                      <span className="text-xs font-medium">{inc.risk_score.toFixed(1)}</span>
                    </div>
                  </Td>
                  <Td className="max-w-64 truncate text-muted-foreground">{inc.reason || "Rule thresholds exceeded"}</Td>
                  <Td>
                    {inc.ai_explanation ? (
                      <span className="inline-flex items-center gap-1 text-xs text-primary">
                        <Sparkles size={12} /> assessed
                      </span>
                    ) : (
                      <span className="text-xs text-muted-foreground">pending…</span>
                    )}
                  </Td>
                  <Td>
                    <Badge className={incidentColor(inc.status)}>{inc.status}</Badge>
                    {busyId === inc.id && <Spinner className="ml-2 inline h-3 w-3" />}
                  </Td>
                  <Td className="whitespace-nowrap text-muted-foreground">{formatDateTime(inc.updated_at)}</Td>
                </tr>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <div className="flex items-center justify-between text-sm text-muted-foreground">
        <span>{total} incidents</span>
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

      <Drawer open={!!selected} onClose={() => setSelected(null)} title={selected ? "Incident review" : ""}>
        {selected && (
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <Badge className={riskColor(selected.risk_level)}>
                {selected.risk_level} · {selected.risk_score.toFixed(1)}/100
              </Badge>
              <Badge className={incidentColor(selected.status)}>{selected.status}</Badge>
            </div>

            <div className="grid grid-cols-2 gap-3 text-sm">
              <Info k="Transaction" v={short(selected.transaction_id)} mono />
              <Info k="Created" v={formatDateTime(selected.created_at)} />
              <Info k="Updated" v={formatDateTime(selected.updated_at)} />
              <Info k="Reason" v={selected.reason || "Rule thresholds exceeded"} />
            </div>

            <div>
              <h4 className="mb-2 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
                <Sparkles size={13} /> AI assessment
              </h4>
              {selected.ai_explanation ? (
                <p className="rounded-md border border-border/50 bg-muted/40 p-3 text-xs leading-relaxed">
                  {selected.ai_explanation}
                </p>
              ) : (
                <p className="rounded-md border border-border/50 p-3 text-xs text-muted-foreground">
                  AI assessment still in flight — check back shortly.
                </p>
              )}
            </div>

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
      <div className={`mt-0.5 text-sm font-medium ${mono ? "font-mono text-xs" : ""}`}>{v}</div>
    </div>
  );
}

function short(id: string) {
  return id ? id.slice(0, 8) : "-";
}
