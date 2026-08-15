"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useWebSocket } from "@/lib/useWebSocket";
import type { WSEvent } from "@/lib/types";

export interface LiveEvent extends WSEvent {
  seq: number;
}

interface LiveContextValue {
  connected: boolean;
  events: LiveEvent[];
}

const LiveContext = createContext<LiveContextValue>({ connected: false, events: [] });

/**
 * Owns the single WebSocket connection for the whole dashboard session. The
 * socket lives at the layout level, so it stays open across client-side page
 * navigation (Overview -> Transactions -> Incidents ...). Every event that
 * arrives is kept in a small ring buffer; pages consume new ones via
 * useLiveEvents().
 */
export function LiveProvider({ children }: { children: React.ReactNode }) {
  const [events, setEvents] = useState<LiveEvent[]>([]);
  const seqRef = useRef(0);

  const onEvent = useCallback((ev: WSEvent) => {
    const rec: LiveEvent = { ...ev, seq: ++seqRef.current };
    setEvents((prev) => {
      if (prev.length >= 100) return [...prev.slice(-99), rec];
      return [...prev, rec];
    });
  }, []);

  const { connected } = useWebSocket(onEvent);
  const value = useMemo(() => ({ connected, events }), [connected, events]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export function useLiveContext() {
  return useContext(LiveContext);
}

/**
 * Returns the events that arrived after this hook mounted. The cursor is
 * initialized to the newest buffered event on mount, so pre-mount history is
 * not replayed — pages fetch their initial state from the REST API instead.
 * Events that arrive while the page is mounted are returned as soon as they
 * are received.
 */
export function useLiveEvents() {
  const { events } = useLiveContext();
  const cursorRef = useRef<number | null>(null);
  const [fresh, setFresh] = useState<LiveEvent[]>([]);

  useEffect(() => {
    let cursor = cursorRef.current;
    if (cursor == null) {
      const last = events[events.length - 1];
      cursor = last ? last.seq : 0;
      cursorRef.current = cursor;
      setFresh([]);
      return;
    }
    const next = events.filter((e) => e.seq > cursor);
    if (next.length > 0) {
      cursorRef.current = next[next.length - 1].seq;
      setFresh(next);
    } else {
      setFresh([]);
    }
  }, [events]);

  return fresh;
}
