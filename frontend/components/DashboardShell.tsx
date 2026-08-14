"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  LayoutDashboard,
  ListChecks,
  ReceiptText,
  KeyRound,
  Settings2,
  ScrollText,
  ShieldAlert,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { logout, useAuth } from "@/lib/useAuth";
import { Button } from "@/components/ui/button";
import { useWebSocket } from "@/lib/useWebSocket";
import { LiveProvider } from "@/components/LiveProvider";

const nav = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/transactions", label: "Transactions", icon: ReceiptText },
  { href: "/incidents", label: "Incidents", icon: ListChecks },
  { href: "/api-keys", label: "API Keys", icon: KeyRound },
  { href: "/rules", label: "Rules", icon: Settings2 },
  { href: "/audit", label: "Audit Log", icon: ScrollText },
];

export function DashboardShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { user } = useAuth();
  const { connected } = useWebSocket(() => {});

  return (
    <LiveProvider>
      <div className="flex min-h-screen">
      <aside className="fixed inset-y-0 left-0 z-20 flex w-56 flex-col border-r border-border bg-card">
        <div className="flex items-center gap-2.5 border-b border-border/60 px-4 py-4">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary/15 text-primary">
            <ShieldAlert size={18} />
          </div>
          <div>
            <div className="text-sm font-semibold leading-tight">MAFAFD</div>
            <div className="text-[10px] uppercase tracking-wider text-muted-foreground">
              {user?.tenant.name || "Fraud Detection"}
            </div>
          </div>
        </div>

        <nav className="flex-1 space-y-0.5 px-2 py-3">
          {nav.map((item) => {
            const active = item.href === "/" ? pathname === "/" : pathname.startsWith(item.href);
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "flex items-center gap-2.5 rounded-md px-3 py-2 text-sm transition-colors",
                  active
                    ? "bg-primary/15 font-medium text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )}
              >
                <Icon size={16} />
                {item.label}
              </Link>
            );
          })}
        </nav>

        <div className="border-t border-border/60 p-3">
          <div className="mb-2 flex items-center gap-2 text-xs">
            <span
              className={cn(
                "inline-block h-2 w-2 rounded-full",
                connected ? "bg-emerald-400" : "animate-pulse-red bg-red-500",
              )}
            />
            <span className="text-muted-foreground">{connected ? "Live stream connected" : "Reconnecting…"}</span>
          </div>
          <div className="flex items-center justify-between">
            <div className="min-w-0">
              <div className="truncate text-xs font-medium">{user?.email}</div>
              <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{user?.role}</div>
            </div>
            <Button variant="ghost" size="sm" onClick={logout}>
              Sign out
            </Button>
          </div>
        </div>
      </aside>

      <main className="ml-56 flex-1 px-6 py-6">{children}</main>
      </div>
    </LiveProvider>
  );
}
