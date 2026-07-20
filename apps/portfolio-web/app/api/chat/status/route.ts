import { NextResponse } from "next/server";

// Same-origin proxy for the gateway's live status, powering the widget's
// "answered from my homelab" card. Best-effort: any failure is reported as
// offline rather than surfaced as an error.
export const dynamic = "force-dynamic";

export async function GET(): Promise<NextResponse> {
  const gatewayUrl = process.env.CHAT_GATEWAY_URL;
  if (!gatewayUrl) {
    return NextResponse.json({ status: "unconfigured", nats: false }, { status: 200 });
  }
  try {
    const res = await fetch(`${gatewayUrl}/status`, {
      cache: "no-store",
      signal: AbortSignal.timeout(4_000),
    });
    const data = await res.json().catch(() => ({ status: "down", nats: false }));
    return NextResponse.json(data, { status: 200 });
  } catch {
    return NextResponse.json({ status: "down", nats: false }, { status: 200 });
  }
}
