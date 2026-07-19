---
sidebar_position: 1
title: AI chat message
---

# Sequence: AI chat message

What happens from a user's `/ai` message to the assistant's reply. See the
[LINE AI chatbot](/services/line-chatbot) page for the component-level view.

```mermaid
sequenceDiagram
  autonumber
  actor U as LINE user
  participant L as LINE Platform
  participant CF as cloudflared
  participant W as line-webhook
  participant N as NATS
  participant P as consumer-llm-processor
  participant DB as Postgres
  participant RD as Redis
  participant X as LLM provider
  participant R as consumer-reply-line-user

  U->>L: "/ai explain kubernetes"
  L->>CF: webhook (HTTPS)
  CF->>W: POST /webhook
  W->>W: verify X-Line-Signature
  W->>RD: SET chat:ai_session:<uid> (10m)
  W->>N: publish line.chat.ai_request
  Note over W,L: webhook returns 200 immediately;\nit never replies to LINE itself

  N->>P: deliver ai_request (queue group)
  P->>P: debounce burst (≤5s window)
  P->>RD: GET chat:history:<uid>
  alt cache miss
    P->>DB: SELECT recent line_ai_messages
    P->>RD: repopulate cache
  end
  P->>P: classify tier (simple/general/technical/…)
  P->>X: Reply(history, message) — first chain provider
  alt provider errors / rate-limited
    X-->>P: error
    P->>X: next provider in chain
  end
  X-->>P: answer text
  P->>DB: append user + model turns
  P->>N: publish line.chat.reply

  N->>R: deliver reply (queue group)
  R->>L: ReplyMessage(reply_token)
  alt token expired / already used
    R->>L: PushMessage(user_id)
  end
  L->>U: assistant answer
```

## Notes on the tricky steps

- **Step 5–7 (async ack):** the webhook publishes to NATS and returns `200` to
  LINE right away. LINE only requires a fast webhook ack, not the answer — the
  answer comes later via the reply token.
- **Debounce (step 9):** if the user fired several messages, they're merged into
  one request so the LLM is called once. Reminder-path messages skip this.
- **Provider fallback (steps 14–16):** the difficulty tier picks a chain; the
  first provider to succeed wins, and any error (including 429s) falls through to
  the next. A broken classifier defaults to the general chain, so a
  classification failure never blocks the answer.
- **Reply vs push (steps 20–22):** the free reply token is tried first; only if
  it's expired/used does it fall back to the quota-limited push. Long answers are
  split into ≤5 messages to fit one reply-token call.
