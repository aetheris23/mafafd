"use client";

import { useCallback, useEffect, useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Spinner } from "@/components/ui/loading";
import { Table, TBody, Td, Th, THead } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { fetchJSON, formatDateTime, shortId } from "@/lib/utils";

interface AuditRow {
  id: number;
  action: string;
  resource: string;
  resource_id: string;
  actor_user_id?: string;
  detail?: Record<string, unknown> | null;
  created_at: string;
}

export default function AuditPage() {
  const [items, setItems] = useState<AuditRow[]>([]);
  const [page, setPage] = useState(1);
  const [perPage] = useState(50);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchJSON<{ items: AuditRow[] }>(`/api/v1/audit?page=${page}&per_page=${perPage}`);
      setItems(data.items);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, [page, perPage]);

  useEffect(() => {
    load();
  }, [load]);

  const actionTone = (a: string) => {
    if (a.includes("key") || a.includes("rules")) return "border-amber-500/40 bg-amber-500/10 text-amber-400";
    if (a === "login") return "border-sky-500/40 bg-sky-500/10 text-sky-400";
    if (a.includes("incident") || a.includes("transaction")) return "border-emerald-500/40 bg-emerald-500/10 text-emerald-400";
    return "border-border text-muted-foreground";
  };

  return (
    <div className="space-y-4">
      <h1 className="font-mono text-base font-semibold uppercase tracking-wider">Audit Log</h1>
      <Card>
        <CardContent className="p-0">
          <Table>
            <THead>
              <Th>Time</Th>
              <Th>Action</Th>
              <Th>Resource</Th>
              <Th>Resource ID</Th>
              <Th>Actor</Th>
              <Th>Detail</Th>
            </THead>
            <TBody>
              {loading && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center"><Spinner className="mx-auto" /></Td>
                </tr>
              )}
              {!loading && items.length === 0 && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center text-muted-foreground">No audit events.</Td>
                </tr>
              )}
              {items.map((row) => (
                <tr key={row.id}>
                  <Td className="whitespace-nowrap text-muted-foreground">{formatDateTime(row.created_at)}</Td>
                  <Td><Badge className={actionTone(row.action)}>{row.action}</Badge></Td>
                  <Td className="text-muted-foreground">{row.resource}</Td>
                  <Td className="font-mono text-xs text-muted-foreground">{row.resource_id ? shortId(row.resource_id) : "-"}</Td>
                  <Td className="font-mono text-xs text-muted-foreground">{row.actor_user_id ? shortId(row.actor_user_id) : "-"}</Td>
                  <Td className="max-w-56 truncate font-mono text-xs text-muted-foreground">
                    {row.detail ? JSON.stringify(row.detail) : "-"}
                  </Td>
                </tr>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>

      <div className="flex items-center justify-end gap-2 text-sm text-muted-foreground">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
          <ChevronLeft size={14} /> Prev
        </Button>
        <span className="px-1">Page {page}</span>
        <Button variant="outline" size="sm" onClick={() => setPage(page + 1)}>
          Next <ChevronRight size={14} />
        </Button>
      </div>
    </div>
  );
}
