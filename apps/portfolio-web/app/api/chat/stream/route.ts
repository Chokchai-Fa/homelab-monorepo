import { NextRequest, NextResponse } from "next/server";

// Same-origin proxy for the streaming (SSE) chat endpoint. It relays the
// gateway's text/event-stream response body straight through to the browser
// so tokens arrive as they are generated. Like /api/chat it keeps the gateway
// private and forwards the visitor's IP for rate limiting.
export const dynamic = "force-dynamic";

// Above the gateway's own request timeout so its terminating error frame
// reaches the browser rather than an aborted fetch.
const PROXY_TIMEOUT_MS = 70_000;

export async function POST(req: NextRequest): Promise<Response> {
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

  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const cfIp = req.headers.get("cf-connecting-ip");
  if (cfIp) headers["CF-Connecting-IP"] = cfIp;
  const xff = req.headers.get("x-forwarded-for");
  if (xff) headers["X-Forwarded-For"] = xff;

  let res: Response;
  try {
    res = await fetch(`${gatewayUrl}/chat/stream`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        session_id: body.sessionId,
        message: body.message,
      }),
      cache: "no-store",
      signal: AbortSignal.timeout(PROXY_TIMEOUT_MS),
    });
  } catch {
    return NextResponse.json(
      { error: "Chat is temporarily unavailable." },
      { status: 503 }
    );
  }

  // The gateway returns JSON (not SSE) for pre-stream rejections (400/429/503).
  const contentType = res.headers.get("content-type") ?? "";
  if (!res.ok || !contentType.includes("text/event-stream") || !res.body) {
    const data = await res
      .json()
      .catch(() => ({ error: "Chat is temporarily unavailable." }));
    return NextResponse.json(data, { status: res.ok ? 502 : res.status });
  }

  // Pass the event-stream body through unbuffered.
  return new Response(res.body, {
    status: 200,
    headers: {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "X-Accel-Buffering": "no",
    },
  });
}
