import { NextRequest, NextResponse } from "next/server";

// Same-origin proxy between the chat widget and portfolio-chat-gateway.
// Keeping the gateway ClusterIP-only means no CORS and no extra public
// hostname; this handler is the only caller.
export const dynamic = "force-dynamic";

// Slightly above the gateway's own 60s NATS timeout so its error mapping
// (504 vs 503) reaches the widget instead of an aborted fetch.
const PROXY_TIMEOUT_MS = 65_000;

export async function POST(req: NextRequest): Promise<NextResponse> {
  const gatewayUrl = process.env.CHAT_GATEWAY_URL;
  if (!gatewayUrl) {
    return NextResponse.json(
      { error: "Chat is not configured on this deployment." },
      { status: 503 }
    );
  }

  let body: { sessionId?: string; message?: string };
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: "Invalid request body." }, { status: 400 });
  }

  // Forward the visitor's identity so the gateway rate-limits real
  // visitors, not this server's pod IP.
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const cfIp = req.headers.get("cf-connecting-ip");
  if (cfIp) headers["CF-Connecting-IP"] = cfIp;
  const xff = req.headers.get("x-forwarded-for");
  if (xff) headers["X-Forwarded-For"] = xff;

  try {
    const res = await fetch(`${gatewayUrl}/chat`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        session_id: body.sessionId,
        message: body.message,
      }),
      cache: "no-store",
      signal: AbortSignal.timeout(PROXY_TIMEOUT_MS),
    });
    const data = await res
      .json()
      .catch(() => ({ error: "Chat is temporarily unavailable." }));
    return NextResponse.json(data, { status: res.status });
  } catch {
    return NextResponse.json(
      { error: "Chat is temporarily unavailable." },
      { status: 503 }
    );
  }
}
