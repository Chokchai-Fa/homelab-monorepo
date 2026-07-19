---
sidebar_position: 3
title: Firing a reminder
---

# Sequence: firing a reminder

From a `pending` row to a flex-message notification in the user's chat — the
time-based half of the [reminder system](/services/reminder-system). This path
has no reply token (the user didn't just message us), so delivery uses **push**.
Note that subscriber-reminder-notifier never builds a LINE message itself — it
ships the raw reminder facts, and consumer-reply-line-user renders the flex
bubble, the same as it renders every other message shape in the system.

```mermaid
sequenceDiagram
  autonumber
  participant S as worker-reminder-scheduler
  participant DB as Postgres
  participant RD as Redis
  participant Nf as subscriber-reminder-notifier
  participant N as NATS
  participant R as consumer-reply-line-user
  participant L as LINE Platform
  actor U as target user

  loop every TICK (1m)
    S->>DB: UPDATE pending → scheduled WHERE remind_at ≤ now()+5m RETURNING id, remind_at
    S->>RD: SET reminder:fire:<id> PX (remind_at − now, ≥1s)
  end

  Note over RD: TTL elapses at remind_at
  RD-->>Nf: __keyevent@0__:expired → reminder:fire:<id>
  Nf->>DB: UPDATE scheduled → sending WHERE id=<id> RETURNING …
  alt already claimed (race / duplicate)
    DB-->>Nf: 0 rows → stop
  end
  Nf->>DB: SELECT display_name FROM line_users (creator)
  Nf->>N: line.chat.reply {reminder: {message, creator_display_name, remind_at}, reminder_id, no reply_token}
  N->>R: deliver reply
  R->>R: build flex bubble (JSON) from the reminder facts
  R->>L: PushMessage(target_user_id, flex)
  L->>U: ⏰ reminder notification
  R->>N: line.chat.delivery {reminder_id, ok / error_code}
  N->>Nf: deliver ack
  alt ok
    Nf->>DB: UPDATE → sent
  else 429 quota
    Nf->>DB: UPDATE → failed, fail_reason=quota_429
  end
```

## Recovery paths (not shown above)

Redis expiry events are **at-most-once**, and an armed key can be evicted under
memory pressure. The scheduler's recovery pass (same 1-minute tick) is the
safety net:

```mermaid
sequenceDiagram
  autonumber
  participant S as worker-reminder-scheduler
  participant DB as Postgres
  participant RD as Redis

  Note over S: every tick, after arming
  S->>DB: SELECT scheduled rows overdue > 2m
  loop each overdue id
    S->>RD: EXISTS reminder:fire:<id>?
    alt key missing (lost expiry / evicted)
      S->>RD: SET reminder:fire:<id> PX 1s  (re-fire now)
    end
  end
  S->>DB: sending stuck > 5m → failed (no_delivery_ack)
  S->>DB: failed & retryable & cooled down 1h & <7d overdue → pending
```

## Notes

- **The claim is atomic** (`UPDATE … WHERE status='scheduled' RETURNING`). If
  two events or two replicas race, only one gets rows; the other stops. This is
  what makes "fire exactly once" hold without broker durability.
- **Flex rendering lives in consumer-reply-line-user, not the notifier.** The
  notifier's job ends at "here are the facts"; the reply consumer is the only
  service that knows LINE message shapes (flex, quick-replies, text splitting).
  This is the same separation the [creation flow](/diagrams/sequence-reminder-create)
  already follows — consumer-reminder never touches LINE directly either.
- **No reply token → push.** Because firing isn't a response to a user message,
  the notifier sends with an empty reply token, so consumer-reply-line-user
  goes straight to push — which is quota-limited. See
  [push-quota 429](/runbooks/push-quota-429).
- **The delivery-ack roundtrip is the only way failures become visible.**
  Without it the notifier couldn't tell a landed push from a dropped one, and the
  scheduler couldn't retry quota failures.
- **Everything reconciles from Postgres.** Lose Redis entirely and the next
  scheduler tick re-arms every `scheduled` row — no reminders are lost, only
  slightly delayed.
