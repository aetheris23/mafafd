"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  KeyRound,
  LayoutDashboard,
  ListChecks,
  Menu,
  ReceiptText,
  ScrollText,
  Settings2,
  X,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { logout, useAuth } from "@/lib/useAuth";
import { Button } from "@/components/ui/button";
import { Logo } from "@/components/Logo";
import { LiveProvider, useLiveContext } from "@/components/LiveProvider";

const nav = [
  { href: "/", label: "Overview", icon: LayoutDashboard },
  { href: "/transactions", label: "Transactions", icon: ReceiptText },
  { href: "/incidents", label: "Incidents", icon: ListChecks },
  { href: "/api-keys", label: "API Keys", icon: KeyRound },
  { href: "/rules", label: "Rules", icon: Settings2 },
  { href: "/audit", label: "Audit Log", icon: ScrollText },
];

export function DashboardShell({ children }: { children: React.ReactNode }) {
  return (
    <LiveProvider>
      <ShellInner>{children}</ShellInner>
    </LiveProvider>
  );
}

function ShellInner({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { user } = useAuth();
  const { connected } = useLiveContext();
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    setMobileOpen(false);
  }, [pathname]);

  return (
    <div className="flex min-h-screen">
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 backdrop-blur-sm md:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden="true"
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-40 flex w-72 flex-col border-r border-border bg-card transition-transform duration-200 md:w-56 md:translate-x-0",
          mobileOpen ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex items-center justify-between border-b border-border/60 px-4 py-4">
          <Link href="/" className="flex items-center gap-2.5">
            <Logo size={26} className="text-primary" />
            <div>
              <div className="font-mono text-sm font-semibold uppercase tracking-[0.2em] text-foreground">
                MAFAFD
              </div>
              <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                {user?.tenant.name || "Fraud Detection"}
              </div>
            </div>
          </Link>
          <button
            className="rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground md:hidden"
            onClick={() => setMobileOpen(false)}
            aria-label="Close menu"
          >
            <X size={16} />
          </button>
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
                  "flex items-center gap-2.5 rounded-md px-3 py-2 font-mono text-[11px] uppercase tracking-wider transition-colors",
                  active
                    ? "bg-primary/15 font-medium text-primary"
                    : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )}
              >
                <Icon size={15} />
                {item.label}
              </Link>
            );
          })}
        </nav>

        <div className="border-t border-border/60 p-3">
          <div className="mb-2 flex items-center gap-2 font-mono text-[11px]">
            <span
              className={cn(
                "inline-block h-2 w-2 rounded-full",
                connected ? "bg-emerald-400" : "animate-pulse-red bg-red-500",
              )}
            />
            <span className="text-muted-foreground">
              {connected ? "LIVE STREAM" : "RECONNECTING"}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <div className="min-w-0">
              <div className="truncate text-xs font-medium">{user?.email}</div>
              <div className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground">
                {user?.role}
              </div>
            </div>
            <Button variant="ghost" size="sm" onClick={logout}>
              Sign out
            </Button>
          </div>
        </div>
      </aside>

      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col md:ml-56">
        {/* Mobile top bar */}
        <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-border bg-background/90 px-4 py-3 backdrop-blur md:hidden">
          <button
            className="rounded p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground"
            onClick={() => setMobileOpen(true)}
            aria-label="Open menu"
          >
            <Menu size={19} />
          </button>
          <Link href="/" className="flex items-center gap-2">
            <Logo size={22} className="text-primary" />
            <span className="font-mono text-sm font-semibold uppercase tracking-[0.2em] text-foreground">
              MAFAFD
            </span>
          </Link>
          <span
            className={cn(
              "ml-auto inline-block h-2 w-2 rounded-full",
              connected ? "bg-emerald-400" : "animate-pulse-red bg-red-500",
            )}
            title={connected ? "Live stream connected" : "Reconnecting…"}
          />
        </header>

        <main className="flex-1 px-4 py-5 sm:px-6 sm:py-6">{children}</main>
      </div>
    </div>
  );
}
