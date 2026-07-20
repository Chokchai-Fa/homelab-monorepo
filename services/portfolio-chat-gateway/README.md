# Portfolio Chat Gateway

HTTP entry point for the portfolio website's "Ask AI about me" chat widget —
the web-channel equivalent of `line-webhook`. It validates and rate-limits
visitor messages, relays each one to `consumer-llm-processor` over NATS
request-reply (`portfolio.chat.ai_request`) and returns the answer.

## What it does

- `POST /chat` — body `{"session_id": "<uuid>", "message": "..."}`, returns
  `{"text": "..."}`. The session ID is a browser-generated UUID; the
  consumer stores conversation history under `web:<session_id>`.
- `GET /healthz` — liveness probe.
- Per-visitor-IP rate limiting (`RATE_LIMIT_PER_MIN`, default 10/min) and a
  message size cap (`MAX_MESSAGE_CHARS`, default 1000) protect the free-tier
  LLM quotas behind it.
- Error mapping: 400 invalid input, 429 rate-limited, 503 NATS unavailable,
  504 LLM timeout — the widget shows these as friendly bubbles.

The service is ClusterIP-only. Its sole caller is portfolio-web's Next.js
`/api/chat` route handler (same-origin proxy), so the gateway never faces
the public internet and no CORS or new tunnel hostname is needed. The proxy
forwards `CF-Connecting-IP` so rate limiting sees the real visitor, not the
portfolio-web pod.

## Required environment variables

```bash
PORT=8081
NATS_URL=nats://localhost:4222
NATS_USER=
NATS_PASSWORD=

# Optional tuning
REQUEST_TIMEOUT=60s       # NATS request-reply timeout (> consumer's 55s)
RATE_LIMIT_PER_MIN=10     # per-visitor-IP messages per minute
MAX_MESSAGE_CHARS=1000
```

## Local development

```bash
cd services/portfolio-chat-gateway
go mod tidy
go run .

curl -s localhost:8081/chat -H 'Content-Type: application/json' \
  -d '{"session_id":"00000000-0000-4000-8000-000000000000","message":"What does he do at LINE?"}'
```

A running NATS plus `consumer-llm-processor` are needed for real answers;
without them the gateway answers 503/504.

## Message flow

1. Widget → portfolio-web `/api/chat` (same origin) → this gateway.
2. Gateway publishes a NATS request on `portfolio.chat.ai_request`.
3. `consumer-llm-processor` (webchat channel, portfolio persona) answers via
   the reply inbox; the gateway returns it as JSON.
