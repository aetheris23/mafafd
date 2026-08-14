"use client";

import { createContext, useContext, useMemo, useState } from "react";
import { useWebSocket } from "@/lib/useWebSocket";
import type { WSEvent } from "@/lib/types";

const LiveContext = createContext<{ last: WSEvent | null }>({ last: null });

export function LiveProvider({ children }: { children: React.ReactNode }) {
  const [last, setLast] = useState<WSEvent | null>(null);
  useWebSocket(setLast);
  const value = useMemo(() => ({ last }), [last]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLiveEvent() {
  return useContext(LiveContext);
}
