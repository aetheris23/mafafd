import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "MAFAFD — Fraud & Anomaly Detection",
  description: "Multi-Tenant AI Financial Anomaly & Fraud Detector",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body>{children}</body>
    </html>
  );
}
