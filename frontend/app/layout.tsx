import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "MAFAFD — AI Financial Anomaly & Fraud Detection",
  description:
    "Multi-Tenant AI Financial Anomaly & Fraud Detector. Real-time rule engine, machine-learning risk scoring and live anomaly stream.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <head>
        <link rel="preconnect" href="https://fonts.googleapis.com" />
        <link rel="preconnect" href="https://fonts.gstatic.com" crossOrigin="anonymous" />
        <link
          href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=Space+Grotesk:wght@400;500;600;700&display=swap"
          rel="stylesheet"
        />
      </head>
      <body>{children}</body>
    </html>
  );
}
