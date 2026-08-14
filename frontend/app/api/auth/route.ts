import { NextResponse } from "next/server";
import { cookies } from "next/headers";

const COOKIE = "mafafd_token";
const TTL = 12 * 3600;
const GW = () => process.env.GATEWAY_INTERNAL_URL || "http://localhost:8081";

export async function POST(request: Request) {
  let body: { email?: string; password?: string } = {};
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid body" }, { status: 400 });
  }
  if (!body.email || !body.password) {
    return NextResponse.json({ error: "email and password required" }, { status: 400 });
  }

  const res = await fetch(`${GW()}/api/v1/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ email: body.email, password: body.password }),
    cache: "no-store",
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    return NextResponse.json({ error: data.error || "authentication failed" }, { status: res.status });
  }
  if (!data.token) {
    return NextResponse.json({ error: "authentication failed" }, { status: 500 });
  }

  const response = NextResponse.json({ user: data.user });
  response.cookies.set(COOKIE, data.token, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: TTL,
  });
  return response;
}

export async function GET() {
  const store = await cookies();
  const token = store.get(COOKIE)?.value;
  if (!token) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }
  const res = await fetch(`${GW()}/api/v1/auth/me`, {
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }
  const data = await res.json();
  return NextResponse.json({ user: data });
}

export async function DELETE() {
  const response = NextResponse.json({ ok: true });
  response.cookies.set(COOKIE, "", { httpOnly: true, path: "/", maxAge: 0 });
  return response;
}

export async function PUT() {
  const store = await cookies();
  const token = store.get(COOKIE)?.value;
  if (!token) {
    return NextResponse.json({ error: "unauthenticated" }, { status: 401 });
  }
  const res = await fetch(`${GW()}/api/v1/auth/ws-token`, {
    method: "POST",
    headers: { authorization: `Bearer ${token}` },
    cache: "no-store",
  });
  if (!res.ok) {
    return NextResponse.json({ error: "ws token denied" }, { status: 401 });
  }
  const data = await res.json();
  return NextResponse.json({ token: data.token });
}
