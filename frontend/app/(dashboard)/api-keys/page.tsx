"use client";

import { useCallback, useEffect, useState } from "react";
import { Copy, KeyRound, Plus, Trash2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/loading";
import { Table, TBody, Td, Th, THead } from "@/components/ui/table";
import type { APIKey, APIKeyCreated } from "@/lib/types";
import { cn, fetchJSON, formatDateTime } from "@/lib/utils";

export default function ApiKeysPage() {
  const [keys, setKeys] = useState<APIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<APIKeyCreated | null>(null);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await fetchJSON<{ items: APIKey[] }>("/api/v1/keys");
      setKeys(data.items);
    } catch {
      /* ignore */
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const create = async () => {
    if (!name.trim()) return;
    setCreating(true);
    try {
      const data = await fetchJSON<APIKeyCreated>("/api/v1/keys", {
        method: "POST",
        body: JSON.stringify({ name: name.trim() }),
      });
      setCreated(data);
      setName("");
      await load();
    } catch (e) {
      alert(e instanceof Error ? e.message : "Failed to create key");
    } finally {
      setCreating(false);
    }
  };

  const revoke = async (id: string) => {
    if (!confirm("Revoke this API key? It will stop working immediately.")) return;
    await fetchJSON<{ ok: boolean }>(`/api/v1/keys/${id}`, { method: "PATCH" });
    await load();
  };

  const copyKey = async () => {
    if (!created) return;
    await navigator.clipboard.writeText(created.key);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="space-y-4">
      <h1 className="font-mono text-base font-semibold uppercase tracking-wider">API Keys</h1>

      <Card>
        <CardHeader>
          <CardTitle>Create a key</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex max-w-lg gap-2">
            <Input placeholder="e.g. staging server" value={name} onChange={(e) => setName(e.target.value)} />
            <Button onClick={create} disabled={creating || !name.trim()}>
              {creating ? <Spinner /> : <Plus size={15} />}
              Create
            </Button>
          </div>
          <p className="mt-2 text-xs text-muted-foreground">
            Keys are shown once and stored hashed (SHA-256) — treat them like passwords.
          </p>
        </CardContent>
      </Card>

      {created && (
        <Card className="border-primary/40">
          <CardHeader>
            <CardTitle className="text-primary">Key created — copy it now</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <code className="flex-1 break-all rounded-md border border-border bg-background px-3 py-2 font-mono text-xs">
                {created.key}
              </code>
              <Button variant="secondary" size="sm" onClick={copyKey}>
                {copied ? "Copied" : <Copy size={14} />}
              </Button>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              Send as <code className="rounded bg-muted px-1">X-Api-Key</code>. This is the only time the full key is shown.
            </p>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          <Table>
            <THead>
              <Th>Name</Th>
              <Th>Prefix</Th>
              <Th>Created</Th>
              <Th>Last used</Th>
              <Th>Status</Th>
              <Th className="text-right">Action</Th>
            </THead>
            <TBody>
              {loading && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center"><Spinner className="mx-auto" /></Td>
                </tr>
              )}
              {!loading && keys.length === 0 && (
                <tr>
                  <Td colSpan={6} className="py-8 text-center text-muted-foreground">No API keys yet.</Td>
                </tr>
              )}
              {keys.map((k) => (
                <tr key={k.id} className={cn(k.revoked_at && "opacity-50")}>
                  <Td className="flex items-center gap-2 font-medium">
                    <KeyRound size={14} className="text-muted-foreground" /> {k.name}
                  </Td>
                  <Td className="font-mono text-xs text-muted-foreground">{k.key_prefix}…</Td>
                  <Td className="text-muted-foreground">{formatDateTime(k.created_at)}</Td>
                  <Td className="text-muted-foreground">{formatDateTime(k.last_used_at)}</Td>
                  <Td>
                    {k.revoked_at ? (
                      <Badge className="border-red-500/40 bg-red-500/10 text-red-400">revoked</Badge>
                    ) : (
                      <Badge className="border-emerald-500/40 bg-emerald-500/10 text-emerald-400">active</Badge>
                    )}
                  </Td>
                  <Td className="text-right">
                    {!k.revoked_at && (
                      <Button variant="ghost" size="sm" className="text-red-400" onClick={() => revoke(k.id)}>
                        <Trash2 size={14} /> Revoke
                      </Button>
                    )}
                  </Td>
                </tr>
              ))}
            </TBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  );
}
