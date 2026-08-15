"use client";

import { cn } from "@/lib/utils";

/**
 * Minimal retro mark: a terminal window (rounded square) holding an ascending
 * bar chart (finance / analytics) crossed by a live signal line (AI) and a
 * blinking block cursor (the console). Keeps the AI + finance identity while
 * staying flat, geometric and simple.
 */
export function Logo({ size = 28, className }: { size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
      className={cn("shrink-0", className)}
    >
      <rect x="1.5" y="1.5" width="29" height="29" rx="5" stroke="currentColor" strokeWidth="1.6" opacity="0.95" />
      {/* ascending bars — finance / volume */}
      <rect x="6.5" y="18.5" width="3" height="6.5" fill="currentColor" opacity="0.45" />
      <rect x="11.5" y="15" width="3" height="10" fill="currentColor" opacity="0.7" />
      <rect x="16.5" y="10.5" width="3" height="14.5" fill="currentColor" />
      {/* live signal line — AI analysis */}
      <path
        d="M6.5 21.5 L12 16.5 L16.5 12 L21.5 7"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
        opacity="0.9"
      />
      {/* blinking console cursor */}
      <rect x="23.8" y="4.5" width="2.8" height="5.2" fill="currentColor" className="animate-blink" />
    </svg>
  );
}
