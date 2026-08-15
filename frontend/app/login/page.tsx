"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/loading";
import { Logo } from "@/components/Logo";
import { login } from "@/lib/useAuth";

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const router = useRouter();
  const search = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(email, password);
      router.replace(search.get("next") || "/");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-md border border-primary/30 text-primary shadow-retro">
            <Logo size={34} />
          </div>
          <h1 className="font-mono text-xl font-semibold uppercase tracking-[0.25em] text-foreground">
            MAFAFD
          </h1>
          <p className="mt-1.5 text-sm text-muted-foreground">
            Multi-Tenant AI Financial Anomaly & Fraud Detector
          </p>
        </div>

        <form onSubmit={submit} className="space-y-4 rounded-md border border-border bg-card p-6 shadow-retro">
          <div className="space-y-1.5">
            <label className="font-mono text-[11px] font-medium uppercase tracking-wider text-muted-foreground">Email</label>
            <Input
              type="email"
              required
              autoComplete="username"
              placeholder="you@company.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <label className="font-mono text-[11px] font-medium uppercase tracking-wider text-muted-foreground">Password</label>
            <Input
              type="password"
              required
              autoComplete="current-password"
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>

          {error && (
            <div className="rounded border border-red-500/40 bg-red-500/10 px-3 py-2 font-mono text-xs text-red-400">
              {error}
            </div>
          )}

          <Button type="submit" disabled={busy} className="w-full">
            {busy ? <Spinner /> : null}
            {busy ? "Signing in…" : "Sign in"}
          </Button>
        </form>

        <p className="mt-6 text-center font-mono text-[11px] text-muted-foreground">
          Demo: admin@acme.dev / Acme-Admin-2026! · platform@mafafd.dev / Mafafd-Platform-2026!
        </p>
      </div>
    </div>
  );
}
