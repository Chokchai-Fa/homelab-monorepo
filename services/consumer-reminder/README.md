# consumer-reminder

Owns the reminder business logic (except firing): the chat flow and the
`line_users` + `reminders` tables. All language work happens upstream in
consumer-llm-processor - events arrive here with `message` and `remind_at`
(RFC3339, +07:00) already extracted; this service never calls an LLM.

## Flow

```
consumer-llm-processor ── line.chat.reminder_request ──▶ ┌──────────────────┐
line-webhook ──────────── line.chat.postback ──────────▶ │ consumer-reminder │──▶ line.chat.reply
             ──────────── line.chat.profile ───────────▶ └──────────────────┘
                                                                 │ INSERT reminders (status=pending)
                                                                 ▼
                                               Postgres ◀── worker-reminder-scheduler / subscriber-reminder-notifier
```

A user starts with `/reminder` or `ตั้งเตือน...` (or natural phrasing, which
consumer-llm-processor's classifier detects); llm-processor extracts the
details and hands off here. The conversation:

1. **Target** — เตือนตัวเอง / เตือนคนอื่น (quick-reply buttons). "Someone
   else" lists up to 12 known users from `line_users` by display name.
2. **Details** — free text like "พรุ่งนี้ 9 โมง กินยา" routes through
   llm-processor's extractor and arrives pre-parsed; missing parts get
   re-asked.
3. **Confirm** — preview + ยืนยัน / แก้ไข / ยกเลิก. Confirm inserts a
   `pending` row in `reminders`; firing is the schedulers' job.

Flow state lives in Redis (`chat:reminder_flow:<uid>`, TTL 10m). Every step
also refreshes `chat:ai_session:<uid>` so line-webhook keeps forwarding the
user's free text; llm-processor checks the flow key's existence to route
that text here (extraction, no chat LLM, no debounce).

## Environment

| Var | Default | Notes |
|---|---|---|
| `NATS_URL` / `NATS_USER` / `NATS_PASSWORD` | nats default | |
| `DATABASE_URL` | (required) | owns line_users + reminders DDL |
| `REDIS_ADDR` / `REDIS_USERNAME` / `REDIS_PASSWORD` | localhost:6379 | flow state (fatal if unreachable) |
| `FLOW_TTL` | 10m | conversation + session TTL |
