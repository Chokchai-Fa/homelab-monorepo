---
sidebar_position: 2
title: Creating a reminder
---

# Sequence: creating a reminder

From "remind me tomorrow 9am to take medicine" to a `pending` row in the
`reminders` table. This is the conversational half of the
[reminder system](/services/reminder-system); the firing half is the
[next sequence](/diagrams/sequence-reminder-fire).

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
  R->>U: "ตั้งเตือน ตัวเอง: กินยา เวลา 20/07 09:00 — ยืนยันไหม?"

  U->>W: taps "Confirm" (postback)
  W->>N: line.chat.postback
  N->>C: deliver postback
  C->>DB: INSERT reminders (status=pending)
  C->>RD: DEL chat:reminder_flow:<uid>
  C->>N: line.chat.reply ("บันทึกแล้ว ⏰")
  N->>R: deliver reply
  R->>U: confirmation
```

## Notes

- **Extraction lives in the LLM processor (steps 4–6)**, not in
  consumer-reminder. The reminder service receives structured
  `{message, remind_at}` and never calls an LLM itself. If extraction found only
  part (say, no time), consumer-reminder asks the user for the missing piece
  during `await_details`.
- **"Someone else" branch:** picking *Someone else* instead of *Myself* inserts
  an `await_user` step that lists known users from `line_users` as quick-reply
  buttons (display names, not ids), then proceeds to details/confirm.
- **Free-text steps route back through the webhook.** While
  `chat:reminder_flow:<uid>` exists, the webhook keeps forwarding the user's
  typed text (via the refreshed `chat:ai_session`), and the LLM processor routes
  it to consumer-reminder rather than answering it as chat.
- **The reply token is free here.** Every step in creation is a direct response
  to a user action, so it uses the reply token — no push quota is consumed.
  Firing later is what needs push (next page).
