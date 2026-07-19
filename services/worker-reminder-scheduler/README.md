# worker-reminder-scheduler

A `go-cron` (gocron/v2) poller that turns due `reminders` rows into expiring
Redis keys (`reminder:fire:<id>`) for `subscriber-reminder-notifier` to fire,
and repairs everything that can drift in that relay.

## Pass (every `TICK`, default 1m)

1. **Arm** — claim `pending` rows due within `ARM_HORIZON` (default 5m,
   atomic `UPDATE ... RETURNING`), `SET reminder:fire:<id>` with a TTL equal
   to the time left until `remind_at` (clamped to ≥1s). Redis failure reverts
   the row to `pending` for the next tick.
2. **Recover lost expiry** — Redis pub/sub expiry events are at-most-once,
   and `allkeys-lru` may evict an armed key under memory pressure. Any
   `scheduled` row whose fire time passed >2m ago with no live Redis key
   gets re-armed with a 1s TTL, re-firing through the normal path.
3. **Repair delivery status** — `sending` rows stuck >5m (no delivery ack
   arrived) become `failed`. `failed` rows with a retryable reason
   (`quota_429`, `no_delivery_ack`) go back to `pending` after a 1h cooldown,
   unless they're more than 7 days overdue (abandoned rather than burning
   next month's push quota on stale content).

All time comparisons use Postgres `now()`, not the Pi's clock — only the
Redis TTL computation uses the local clock, and that's clamped to never go
negative.

`replicas: 1` in the deployment, though the atomic claim makes an accidental
second replica harmless rather than required to avoid.

## Environment

| Var | Default |
|---|---|
| `DATABASE_URL` | (required) |
| `REDIS_ADDR` / `REDIS_USERNAME` / `REDIS_PASSWORD` | localhost:6379 |
| `ARM_HORIZON` | 5m |
| `TICK` | 1m |
