---
sidebar_position: 2
title: Portfolio web chatbot
---

# Portfolio web chatbot

The **"Ask AI about me"** widget on the
[portfolio site](https://portfolio.chokchai-dev.xyz) lets a visitor ask about
Chokchai's experience, skills, research and projects and get an answer from the
same AI pipeline that powers the LINE bot. It reuses
**consumer-llm-processor** wholesale; the only new piece is an HTTP entry point,
**portfolio-chat-gateway**, that bridges the browser to NATS.

```mermaid
flowchart LR
  v([Web visitor])
  v -->|chat widget| pw[portfolio-web]
  pw -->|/api/chat proxy| gw[portfolio-chat-gateway]
  gw <-->|request-reply<br/>portfolio.chat.ai_request| n{{NATS}}
  n <--> llm[consumer-llm-processor]
  llm <-->|providers| ext[[Gemini · Groq · OpenRouter]]
  llm --- pg[(Postgres<br/>history)]
  llm --- rd[(Redis<br/>cache)]
```

For the full step-by-step, see the
[portfolio chat sequence](/diagrams/sequence-portfolio-chat).

## Why request-reply, not fire-and-forget

The LINE channel is asynchronous: LINE only needs a fast webhook ack, and the
answer is delivered later by a separate egress service (consumer-reply-line-user)
over a reply token. A **web visitor is different — they are holding an open HTTP
request** and expect the answer on it.

So the web channel uses **NATS request-reply**: the gateway calls
`nc.Request("portfolio.chat.ai_request", …)` and consumer-llm-processor answers
with `msg.Respond(…)` on the auto-generated reply inbox. There is **no reply
subject, no correlation bookkeeping, and no downstream delivery service** — the
answer travels straight back to the waiting gateway. This is the single
architectural difference from the LINE flow; everything downstream (classifier,
provider chains, conversation memory) is identical.

## Components

### portfolio-web
The Next.js portfolio site. Two chat-related pieces:

- **`components/chat/ChatWidget.tsx`** — a floating client-side widget
  (suggested-question chips, typing indicator, "clear chat"). It generates a
  session UUID once and keeps it in `localStorage`.
- **`app/api/chat/route.ts`** — a same-origin **route handler** that proxies the
  widget's POST to the gateway's in-cluster URL (`CHAT_GATEWAY_URL`). Keeping the
  call server-side means the gateway needs no public hostname and there is no
  CORS. It forwards `CF-Connecting-IP` so the gateway rate-limits the real
  visitor, not the portfolio-web pod.

### portfolio-chat-gateway
A small Go/Echo service — the web channel's **ingress and egress in one**. It:

- exposes `POST /chat` (`{session_id, message}` → `{text}`) and `GET /healthz`;
- **validates** the session id shape and message size, and **rate-limits per
  visitor IP** (token bucket, default 10/min) to protect the free-tier LLM
  quotas behind it;
- relays each accepted message over NATS request-reply and maps failures to
  clean HTTP codes: `400` invalid input, `429` rate-limited, `503` NATS
  unavailable, `504` the pipeline took too long.

It is **ClusterIP-only** — portfolio-web's `/api/chat` proxy is its sole caller,
so it never faces the public internet and stays off the cloudflared tunnel.

### consumer-llm-processor (the `webchat` channel)
A second NATS subscription (`internal/webchat`) on `portfolio.chat.ai_request`,
answered with `msg.Respond`. It shares the difficulty router, provider chains and
conversation store with the LINE path, but:

- uses a **professional portfolio persona** (`PortfolioPersonaInstruction`) with
  Chokchai's résumé facts embedded in the system prompt — including the correct
  Thai spelling of his name and his InCIT 2025 paper — instead of the LINE
  persona;
- stores history under **`web:<session_id>`**, so web conversations never
  collide with LINE user ids in the shared `line_ai_messages` table, and `/reset`
  (the widget's "clear chat") works the same way;
- **skips the LINE-only features** — no debounce, no reminder handoff, no image
  input/generation. If the shared classifier tags a web message as a reminder or
  image request, the channel answers with a short "I'm a Q&A assistant" redirect
  rather than acting on it.

:::note Knowledge is prompt-embedded, for now
The assistant only knows what `PortfolioPersonaInstruction` tells it. When
Chokchai adds something to the site (a new role, project or paper), the fact must
be added to that prompt too. Retrieval over the résumé/site content (RAG) is a
planned later phase; until then, "the AI didn't know X" usually means "X isn't in
the prompt yet."
:::

## Configuration

| Where | Key | Purpose |
|-------|-----|---------|
| portfolio-web | `CHAT_GATEWAY_URL` | In-cluster gateway base URL for the `/api/chat` proxy |
| portfolio-chat-gateway | `NATS_*` | Connection to NATS (request-reply) |
| portfolio-chat-gateway | `RATE_LIMIT_PER_MIN` | Per-visitor-IP message budget (default 10) |
| portfolio-chat-gateway | `MAX_MESSAGE_CHARS` | Reject oversize messages (default 1000) |
| portfolio-chat-gateway | `REQUEST_TIMEOUT` | NATS round-trip bound; kept above the consumer's generate timeout |
| consumer-llm-processor | *(shared)* | Same env as the LINE path — one process serves both channels |

## Failure behavior

- **NATS down / no responder** → gateway returns `503`; the widget shows a
  friendly "temporarily unavailable" bubble. The portfolio pages themselves are
  static and stay up regardless.
- **Pipeline slow** → `504` after `REQUEST_TIMEOUT`; the consumer's own generate
  timeout is set lower so it usually returns a friendly answer first.
- **Abuse / quota protection** → per-IP rate limit at the gateway plus the
  message-size cap; the provider fallback chain absorbs individual LLM 429s.
