# subscriber-reminder-notifier

Subscribes to Redis key-expiry events (`__keyevent@0__:expired`) and turns
each expired `reminder:fire:<id>` key into a LINE flex message, via
`consumer-reply-line-user`.

## Flow

```
Redis "reminder:fire:<id>" expires
  → claim reminder (status: scheduled → sending, no-op if already claimed)
  → load creator's display name
  → build flex bubble (typed structs, never string-concatenated)
  → publish line.chat.reply {flex, reminder_id} (no reply token → push)
  ← line.chat.delivery ack from consumer-reply-line-user
  → mark sent, or failed with a reason (quota_429 on HTTP 429)
```

Requires `notify-keyspace-events Ex` on the Redis server (set declaratively
in the deployment; this service also best-effort `CONFIG SET`s it at startup
as a fallback for a manual `CONFIG RESET`).

Every failure path before publish reverts the row to `scheduled` so
`worker-reminder-scheduler`'s recovery pass re-arms it - this service never
retries on its own. `replicas: 1`: Redis pub/sub broadcasts to every
subscriber (not load-balanced), and while the Postgres claim makes a second
replica harmless, it would just be redundant work.

## Environment

| Var | Default |
|---|---|
| `NATS_URL` / `NATS_USER` / `NATS_PASSWORD` | nats default |
| `DATABASE_URL` | (required) |
| `REDIS_ADDR` / `REDIS_USERNAME` / `REDIS_PASSWORD` | localhost:6379 |
