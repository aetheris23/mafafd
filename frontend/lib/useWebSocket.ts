"use client";

import { useEffect, useRef, useState } from "react";
import type { WSEvent } from "@/lib/types";

/**
 * Connects to the gateway WebSocket live stream.
 * A short-lived, WS-scoped token is minted server-side (/api/auth/ws) so the
 * long-lived httpOnly session cookie never has to reach client JS.
 */
export function useWebSocket(onEvent: (ev: WSEvent) => void) {
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const handlerRef = useRef(onEvent);
  handlerRef.current = onEvent;

  useEffect(() => {
    let disposed = false;
    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    const connect = async () => {
      if (disposed) return;
      try {
        const res = await fetch("/api/auth", { method: "PUT", cache: "no-store" });
        if (!res.ok) throw new Error("no ws session");
        const { token } = await res.json();
        if (disposed) return;

        const wsUrl = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8081";
        ws = new WebSocket(`${wsUrl}/api/v1/ws?token=${encodeURIComponent(token)}`);
        wsRef.current = ws;

        ws.onopen = () => setConnected(true);
        ws.onclose = () => {
          setConnected(false);
          if (!disposed) reconnectTimer = setTimeout(connect, 3000);
        };
        ws.onerror = () => ws?.close();
        ws.onmessage = (msg) => {
          try {
            const ev = JSON.parse(msg.data) as WSEvent;
            if (ev.event === "ping") return;
            handlerRef.current(ev);
          } catch {
            /* ignore malformed frame */
          }
        };
      } catch {
        if (!disposed) reconnectTimer = setTimeout(connect, 5000);
      }
    };

    connect();
    return () => {
      disposed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      ws?.close();
    };
  }, []);

  return { connected };
}
