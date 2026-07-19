---
sidebar_position: 2
title: Reminder lifecycle
---

# Sequence: reminder lifecycle

The two halves of the [reminder system](/services/reminder-system) in one
place: **creating** a reminder (conversational, reply-token based) and
**firing** it (time-based, push based).

## Creating a reminder

From "remind me tomorrow 9am to take medicine" to a `pending` row in the
`reminders` table.

```mermaid
sequenceDiagram
  autonumber
  actor U as LINE user
  participant W as line-webhook
  participant N as NATS
  participant P as consumer-llm-processor
  participant C as consumer-reminder
  participant RD as Redis
  participant DB as Postgres
  participant R as consumer-reply-line-user

  U->>W: "เตือนพรุ่งนี้ 9 โมง กินยา"
  W->>N: line.chat.ai_request
  N->>P: deliver
  P->>P: classify → tier "reminder"
  P->>P: extract {message:"กินยา", remind_at:"…T09:00+07:00"}
  Note over P: explicit numeric dates/times<br/>(20/09/2026, เวลา 00.25) are parsed<br/>deterministically and override the LLM
  P->>N: line.chat.reminder_request (with extracted fields)

  N->>C: deliver reminder_request
  C->>RD: SET chat:reminder_flow:<uid> (step=await_target, 10m)
  C->>N: line.chat.reply (quick replies: Myself / Someone else / Cancel)
  N->>R: deliver reply
  R->>U: prompt buttons

  U->>W: taps "Myself" (postback)
  W->>N: line.chat.postback
  N->>C: deliver postback
  Note over C: message + time already extracted →<br/>skip straight to confirmation
  C->>RD: update flow (step=await_confirm)
  C->>N: line.chat.reply (confirm preview + Confirm/Edit/Cancel)
  N->>R: deliver reply
  R->>U: "ตั้งเตือน ตัวเอง: กินยา วันที่ 20/07/2026 เวลา 09:00 น. — ยืนยันไหม?"

  U->>W: taps "Confirm" (postback)
  W->>N: line.chat.postback
  N->>C: deliver postback
  C->>DB: INSERT reminders (status=pending)
  C->>RD: DEL chat:reminder_flow:<uid>
  C->>N: line.chat.reply ("บันทึกแล้ว ⏰")
  N->>R: deliver reply
  R->>U: confirmation
```

### Notes on creation

- **Extraction lives in the LLM processor (steps 4–6)**, not in
  consumer-reminder. The reminder service receives structured
  `{message, remind_at}` and never calls an LLM itself. If extraction found only
  part (say, no time), consumer-reminder asks the user for the missing piece
  during `await_details`.
- **"Someone else" branch:** picking *Someone else* instead of *Myself* inserts
  an `await_user` step that lists known users from `line_users` as quick-reply
  buttons (display names, not ids), then proceeds to details/confirm.
- **Free-text steps route back through the webhook.** While
  `chat:reminder_flow:<uid>` exists, the webhook forwards the user's typed text
  into the AI pipeline (it checks that key directly — no AI session is opened),
  and the LLM processor routes it to consumer-reminder rather than answering it
  as chat. When the flow ends, the key dies and routing stops with it.
- **Typing `ยกเลิก` (or `cancel`) works at any step**, same as the Cancel
  button — see [Commands & keywords](/services/commands).
- **The reply token is free here.** Every step in creation is a direct response
  to a user action, so it uses the reply token — no push quota is consumed.
  Firing later is what needs push (below).

## Managing reminders

`/reminders` (or `ดูเตือน`) lists the creator's upcoming reminders; picking
one offers edit or delete. Both operations lean on the same status-guarded
claims that make firing race-free.

```mermaid
sequenceDiagram
  autonumber
  actor U as LINE user
  participant W as line-webhook
  participant N as NATS
  participant C as consumer-reminder
  participant DB as Postgres
  participant RD as Redis
  participant R as consumer-reply-line-user

  U->>W: "/reminders"
  W->>N: line.chat.ai_request → (llm-processor forwards, no extraction)
  N->>C: line.chat.reminder_request
  C->>DB: SELECT upcoming WHERE creator_id AND status IN (pending, scheduled)
  C->>RD: SET chat:reminder_flow:<uid> (step=manage, 10m)
  C->>N: line.chat.reply (list + one pick-button per reminder)
  N->>R: deliver
  R->>U: numbered list

  U->>W: taps a reminder (postback a=pick)
  W->>N: line.chat.postback
  N->>C: deliver
  C->>N: line.chat.reply (details + แก้ไข / ลบ / ยกเลิก)

  alt delete (a=rdel)
    U->>W: taps "ลบ"
    W->>N: line.chat.postback
    N->>C: deliver
    C->>DB: UPDATE → cancelled WHERE creator AND status IN (pending, scheduled)
    Note over DB,RD: an already-armed reminder:fire:<id> key later expires,<br/>the notifier's claim (WHERE status='scheduled') misses → no fire
    C->>N: line.chat.reply ("ลบแล้วน้า 🗑️")
  else edit (a=redit)
    U->>W: taps "แก้ไข"
    W->>N: line.chat.postback
    N->>C: deliver
    C->>RD: flow (step=await_details, editing_id, message pre-filled)
    C->>N: line.chat.reply ("พิมพ์เวลาใหม่ หรือข้อความใหม่พร้อมเวลา")
    U->>W: "มะรืนนี้ 14:00" (free text → extraction as usual)
    Note over C: confirm preview → user confirms
    C->>DB: UPDATE message, remind_at, status=pending
    Note over DB: scheduler re-arms at the new time on its next tick
    C->>N: line.chat.reply ("แก้ไขแล้ว ⏰")
  end
```

## Firing a reminder

From a `pending` row to a flex-message notification in the user's chat. This
path has no reply token (the user didn't just message us), so delivery uses
**push**. Note that subscriber-reminder-notifier never builds a LINE message
itself — it ships the raw reminder facts, and consumer-reply-line-user renders
the flex bubble, the same as it renders every other message shape in the
system.

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

### Recovery paths (not shown above)

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

### Notes on firing

- **The claim is atomic** (`UPDATE … WHERE status='scheduled' RETURNING`). If
  two events or two replicas race, only one gets rows; the other stops. This is
  what makes "fire exactly once" hold without broker durability.
- **Flex rendering lives in consumer-reply-line-user, not the notifier.** The
  notifier's job ends at "here are the facts"; the reply consumer is the only
  service that knows LINE message shapes (flex, quick-replies, text splitting).
  This is the same separation the creation flow above already follows —
  consumer-reminder never touches LINE directly either.
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
