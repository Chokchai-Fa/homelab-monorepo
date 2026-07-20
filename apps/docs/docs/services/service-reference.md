---
sidebar_position: 3
title: Service reference
---

# Service reference

A one-page cheat sheet for all seven Go services: what each subscribes to,
publishes, and which data stores it touches. Every service is a single-replica
Deployment in the `default` namespace, built from `build/go/Dockerfile`.

## Quick matrix

| Service | Subscribes | Publishes | Postgres | Redis | External |
|---------|-----------|-----------|:--------:|:-----:|----------|
| **line-webhook** | — (HTTP ingress) | `ai_request`, `reply`, `postback`, `profile` | — | ✅ | LINE API (profile, image download) |
| **portfolio-chat-gateway** | — (HTTP ingress) | `portfolio.chat.ai_request` (request-reply) | — | — | — |
| **consumer-llm-processor** | `ai_request`, `portfolio.chat.ai_request` | `reply`, `reminder_request`, request-reply answers | ✅ | ✅ | Gemini/Groq/OpenRouter/CF |
| **consumer-reply-line-user** | `reply` | `delivery` | — | — | LINE API (reply/push) |
| **consumer-reminder** | `reminder_request`, `postback`, `profile` | `reply` | ✅ | ✅ | — |
| **worker-reminder-scheduler** | — (cron) | — | ✅ | ✅ | — |
| **subscriber-reminder-notifier** | `delivery` + Redis expiry | `reply` | ✅ | ✅ | — |

Subjects are the `line.chat.*` family; see the [NATS subject map](/data-services/nats).

## Per-service details

### line-webhook
The only HTTP ingress (Echo, port 8080; `POST /webhook`, `GET /` health).
Verifies LINE signatures, converts events to NATS messages, downloads image
attachments, fetches user profiles (gated by `chat:profile_seen`). **Never
replies to LINE directly.**
- **Env:** `LINE_CHANNEL_SECRET`, `LINE_CHANNEL_ACCESS_TOKEN`, `NATS_*`, `REDIS_*`, `AI_PREFIX` (`/ai`), `AI_SESSION_TTL`, `IMAGE_TTL`, `PORT`.

### portfolio-chat-gateway
The web channel's HTTP entry point (Echo, port 8081; `POST /chat/stream` (SSE,
default), `POST /chat` (whole answer), `GET /status`, `GET /healthz`). Validates
and per-IP rate-limits visitor messages, then bridges each to the AI pipeline
over NATS — **request-reply** (`portfolio.chat.ai_request`) for the unary path,
and a **reply-inbox stream** (`portfolio.chat.ai_request.stream`) for the SSE
path. ClusterIP-only — called solely by portfolio-web's `/api/chat*` proxies. No
datastore of its own.
- **Env:** `NATS_*`, `PORT` (8081), `RATE_LIMIT_PER_MIN` (10), `MAX_MESSAGE_CHARS` (1000), `REQUEST_TIMEOUT` (60s).

### consumer-llm-processor
The AI brain, shared by both channels: classify → route to a provider chain →
answer, with conversation memory and image generation. Detects reminder intent
and hands off. Serves the LINE `ai_request` subject (fire-and-forget) **and** the
web `portfolio.chat.ai_request` subject (request-reply, professional portfolio
persona, history keyed `web:<session_id>`).
- **Env:** `NATS_*`, `DATABASE_URL`, `REDIS_*`, `GEMINI_API_KEY`, `GEMINI_MODEL` (`gemini-3.1-flash-lite`), optional `GROQ_API_KEY`/`GROQ_MODEL`/`GROQ_CLASSIFIER_MODEL`, `OPENROUTER_API_KEY`/`OPENROUTER_MODEL`/`OPENROUTER_VISION_MODEL`, `CF_ACCOUNT_ID`/`CF_API_TOKEN`/`CF_IMAGE_MODEL`, `DEBOUNCE_WINDOW` (5s), `DEBOUNCE_MAX_WAIT` (15s).
- **Owns:** `line_ai_messages` (shared by LINE users and `web:` sessions).

### consumer-reply-line-user
The only egress, and the only service that builds LINE message shapes. Delivers
replies to LINE — reply token first, push fallback — rendering text / images /
quick-replies, and the reminder flex-bubble template itself from the raw facts
a `reminder` payload carries. Publishes a delivery ack.
- **Env:** `NATS_*`, `LINE_CHANNEL_SECRET`, `LINE_CHANNEL_ACCESS_TOKEN`, `IMAGE_BASE_URL` (public base for generated images).

### consumer-reminder
Owns the reminder conversation flow and the `line_users` + `reminders` tables.
Receives already-extracted `{message, remind_at}` — never calls an LLM.
- **Env:** `NATS_*`, `DATABASE_URL`, `REDIS_*`, `FLOW_TTL` (10m).
- **Owns:** `line_users`, `reminders`.

### worker-reminder-scheduler
gocron loop (every `TICK`, default 1m). Arms `pending` reminders due within
`ARM_HORIZON` (5m) as expiring `reminder:fire:<id>` Redis keys; recovers lost
expiries; repairs stuck `sending` rows; retries retryable failures hourly. No
NATS. All comparisons use Postgres `now()`.
- **Env:** `DATABASE_URL`, `REDIS_*`, `ARM_HORIZON` (5m), `TICK` (1m).

### subscriber-reminder-notifier
Subscribes to Redis key-expiry (`__keyevent@0__:expired`). On an expired
`reminder:fire:<id>`: claim the row (`scheduled → sending`), publish
`line.chat.reply` carrying the raw reminder facts (message, creator display
name, time — no reply token → push, and no LINE-specific rendering happens
here), and record the delivery ack (`sent`, or `failed`/`quota_429`).
- **Env:** `NATS_*`, `DATABASE_URL`, `REDIS_*`.

## Deployment conventions

- **Namespace:** `default`; **replicas:** 1 (single-node Pi; the atomic DB claim
  makes an accidental second replica safe but pointless).
- **Secrets:** `nats-auth`, `redis-auth`, `consumer-llm-processor-secret` (holds
  the shared `DATABASE_URL`), `line-webhook-secret`. See
  [secrets bootstrap](/infrastructure/secrets-bootstrap).
- **DNS resilience:** services making external calls (LLMs, LINE) add a
  `dnsConfig` fallback to a public resolver, because cluster DNS occasionally
  SERVFAILs for external names on this setup.
- **Resources:** Pi-sized — workers/subscriber request ~20m/32Mi, the LLM
  processor and reminder consumer a bit more.
