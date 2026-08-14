"use client";

import { useEffect, useState } from "react";
import type { User } from "@/lib/types";

export function useAuth() {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch("/api/auth", { cache: "no-store" })
      .then(async (r) => {
        if (r.ok) {
          const data = await r.json();
          setUser(data.user);
        }
      })
      .finally(() => setLoading(false));
  }, []);

  return { user, loading };
}

export async function login(email: string, password: string) {
  const res = await fetch("/api/auth", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || "Login failed");
  return data.user;
}

export async function logout() {
  await fetch("/api/auth", { method: "DELETE" });
  window.location.href = "/login";
}
