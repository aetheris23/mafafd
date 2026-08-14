"use client";

import { useEffect, useState } from "react";
import { RefreshCw, Save } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/input";
import { Spinner } from "@/components/ui/loading";
import { fetchJSON } from "@/lib/utils";

const DEFAULT_RULES = {
  velocity: {
    customer: { window_sec: 600, limit: 5, weight: 0.25 },
    ip: { window_sec: 60, limit: 12, weight: 0.2 },
    card: { window_sec: 600, limit: 3, weight: 0.2 },
    device: { window_sec: 600, limit: 3, weight: 0.1 },
    global_per_minute: { limit: 100, weight: 0.15 },
  },
  geo: { allowlist: ["US", "ID", "SG"], blocklist: [], blocklist_score: 70, unknown_score: 15, weight: 0.15 },
  ip: { blacklist: [], score: 75, weight: 0.2 },
  amount: { max_single: 10000, suspicious_from: 5000, weight: 0.2 },
  currency: { weight: 0.05 },
  decision: { ai_floor: 30, ai_ceiling: 70, incident_threshold: 40 },
};

const REQUIRED_KEYS = ["velocity", "geo", "ip", "amount", "currency", "decision"];

export default function RulesPage() {
  const [text, setText] = useState("");
  const [dirty, setDirty] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  const load = async (data?: unknown) => {
    setLoading(true);
    try {
      let parsed = data;
      if (!parsed) {
        const res = await fetchJSON<{ data: unknown }>("/api/v1/rules");
        parsed = res.data;
      }
      setText(JSON.stringify(parsed, null, 2));
      setDirty(false);
      setError("");
    } catch {
      setError("Could not load rules.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const validate = (): unknown => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(text);
    } catch {
      setError("Invalid JSON — fix the syntax before saving.");
      return null;
    }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      setError("Rules must be a JSON object.");
      return null;
    }
    for (const k of REQUIRED_KEYS) {
      if (!(k in (parsed as Record<string, unknown>))) {
        setError(`Missing required section "${k}".`);
        return null;
      }
    }
    return parsed;
  };

  const save = async () => {
    const parsed = validate();
    if (parsed === null) return;
    setSaving(true);
    setError("");
    try {
      await fetchJSON<{ ok: boolean }>("/api/v1/rules", {
        method: "PUT",
        body: JSON.stringify({ data: parsed }),
      });
      setSaved(true);
      setDirty(false);
      setTimeout(() => setSaved(false), 2000);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const reset = () => {
    load(DEFAULT_RULES);
    setDirty(true);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">Rules Engine</h1>
          <p className="text-sm text-muted-foreground">
            Deterministic pre-AI scoring. Changes propagate to the worker within ~30s (cached) or instantly via cache eviction.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={reset}>
            <RefreshCw size={14} /> Reset defaults
          </Button>
          <Button onClick={save} disabled={saving || !dirty}>
            {saving ? <Spinner /> : <Save size={14} />}
            {saved ? "Saved" : "Save rules"}
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-red-500/40 bg-red-500/10 px-3 py-2 text-xs text-red-400">{error}</div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>rule_config (JSON)</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex justify-center py-10"><Spinner /></div>
          ) : (
            <Textarea
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                setDirty(true);
                setError("");
              }}
              className="min-h-[480px] text-xs leading-relaxed"
              spellCheck={false}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className="grid grid-cols-1 gap-x-8 gap-y-2 text-xs text-muted-foreground md:grid-cols-2">
          <p><span className="font-medium text-foreground">velocity.customer</span> — max txns per customer in window_sec (600s), weight 0.25</p>
          <p><span className="font-medium text-foreground">velocity.ip</span> — max txns per IP in 60s, weight 0.20</p>
          <p><span className="font-medium text-foreground">velocity.card</span> — max txns per card in 600s, weight 0.20</p>
          <p><span className="font-medium text-foreground">velocity.device</span> — max txns per device fingerprint in 600s, weight 0.10</p>
          <p><span className="font-medium text-foreground">velocity.global_per_minute</span> — tenant-wide txns/min, weight 0.15</p>
          <p><span className="font-medium text-foreground">geo</span> — allowlist/blocklist; blocklist adds 70, unknown country 15</p>
          <p><span className="font-medium text-foreground">ip</span> — blacklisted IP adds 75</p>
          <p><span className="font-medium text-foreground">amount</span> — &gt; suspicious_from adds partial, &gt; max_single +70</p>
          <p><span className="font-medium text-foreground">currency</span> — currency mismatch adds 40</p>
          <p><span className="font-medium text-foreground">decision</span> — risk &lt; 30 LOW, 30–70 SUSPICIOUS (→ AI), ≥ 70 HIGH; incident at ≥ 40</p>
        </CardContent>
      </Card>
    </div>
  );
}
