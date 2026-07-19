---
sidebar_position: 4
title: Commands & keywords
---

# Commands & keywords

Everything a user can type at the LINE bot, what it does, and **which service
recognizes it**. Keyword sets that appear in more than one service are
deliberately duplicated copies (monorepo convention) and must stay in sync —
each table notes where.

## AI session

| Command | What it does | Example |
|---------|--------------|---------|
| `/ai` | Starts (or refreshes) an AI session. While active, **every plain message goes to the AI** — no prefix needed. Auto-expires after **10 minutes** of silence; every message slides the timer. | `/ai` |
| `/ai <question>` | Starts the session *and* asks immediately. | `/ai explain kubernetes` |
| `/ai-end` | Ends the session. Plain messages go back to echo mode. | `/ai-end` |

Recognized by **line-webhook** (`AI_PREFIX` env, default `/ai`). The session
lives in Redis as `chat:ai_session:<uid>`.

:::note Only `/ai` opens a session
The reminder flow routes free text to the AI pipeline through its own
`chat:reminder_flow:<uid>` key — finishing a reminder never leaves you stuck
in AI mode.
:::

## Conversation reset

| Command | What it does |
|---------|--------------|
| `/reset` · `ล้าง` · `เริ่มใหม่` | Clears the AI conversation history (Redis cache + Postgres rows) and starts fresh. Sent inside a session, or as `/ai /reset` outside one. |

Recognized by **consumer-llm-processor** (`isResetCommand`). The debouncer
also knows these words so a reset never sits in the message-merge buffer.

## Reminders

| Command / keyword | What it does | Example |
|-------------------|--------------|---------|
| `/reminder` | Starts the reminder conversation (who → what/when → confirm). | `/reminder` |
| `/reminder <details>` | Starts the flow with details pre-filled — extraction runs on the rest of the line. | `/reminder พรุ่งนี้ 9 โมง กินยา` |
| `ตั้งเตือน…` | Thai trigger, no space needed after the keyword. | `ตั้งเตือนพรุ่งนี้เที่ยง ประชุม` |
| *natural language* | No keyword at all: inside an AI session, the LLM classifier detects reminder intent and hands off to the same flow. | `เตือนฉันตอน 3 ทุ่มหน่อย` |
| `ยกเลิก` · `cancel` · `/cancel` | Cancels the reminder flow **at any step** — typed or via the Cancel button. | `ยกเลิก` |
| `/reminders` · `ดูเตือน` · `รายการเตือน` | Lists **your upcoming reminders** (the ones you created, soonest first) with one button per entry — pick one to **edit or delete** it. | `/reminders` |

Trigger keywords are matched identically in **three places** (line-webhook
`isReminderRequest`, consumer-llm-processor `isReminderCommand`,
consumer-reminder `isTrigger`/`isListTrigger`); cancel words in two
(consumer-llm-processor and consumer-reminder `isCancelText`). Change one,
change all.

### Managing reminders

`/reminders` shows up to 12 upcoming reminders, each as a quick-reply button:

1. **Pick** a reminder → its details plus `แก้ไข` / `ลบ` / `ยกเลิก` buttons.
2. **`ลบ` (delete)** — the reminder is cancelled immediately. If its timer was
   already armed, the expiry fires into nothing (status-guarded claim).
3. **`แก้ไข` (edit)** — type a new time (the old message is kept), or a whole
   new "what + when". You get the usual confirm preview before anything is
   saved; the reminder is re-armed at the new time.

Only the **creator** can see, edit, or delete a reminder — including ones
targeted at someone else. Reminders that are firing right now or already
sent/failed no longer appear.

Mid-flow, typing `/reminder` again **restarts** the flow from the beginning —
the escape hatch for a stuck conversation.

### What the extractor understands

Dates and times are resolved in **Asia/Bangkok** (see the
[reminder creation sequence](/diagrams/sequence-reminder#creating-a-reminder)).
Explicit numeric formats are parsed deterministically and win over the LLM:

| Input style | Example | Interpreted as |
|-------------|---------|----------------|
| Date `DD/MM/YYYY` (Christian Era) | `20/09/2026` | 2026-09-20 |
| Date `DD/MM/YYYY` (Buddhist Era, ≥ 2400) | `20/09/2569` | 2026-09-20 |
| Two-digit years (`≥ 60` → BE) | `20/09/69` · `20/09/26` | both 2026-09-20 |
| Era markers | `พ.ศ.` · `ค.ศ.` | force BE / CE for the year |
| Clock with dot or colon | `เวลา 00.25` · `18:30` | 00:25 · 18:30 |
| Hour with clock marker | `เวลา 21 น.` | 21:00 |
| Relative days | `วันนี้` · `พรุ่งนี้` · `มะรืนนี้` | today / +1 / +2 |
| Thai clock words (via LLM) | `9 โมง` · `บ่าย 3` · `3 ทุ่ม` · `ตี 2` · `เที่ยง` · `เที่ยงคืน` | 09:00 · 15:00 · 21:00 · 02:00 · 12:00 · 00:00 |

A bare time that already passed today means **the next occurrence** — at
23:50, `เตือน 0.25` is tonight's 00:25, not 23 hours ago. Multi-line reminder
messages are kept verbatim, every line.

## Echo mode (no session, no keyword)

| Input | Reply |
|-------|-------|
| `hello` / `hi` | greeting |
| `help` | command overview |
| anything else | `You said: <message>` |

Handled entirely by **line-webhook**; nothing is published to the AI pipeline.

## Images

| Action | Requirement | What happens |
|--------|-------------|--------------|
| Send a photo | active AI session | the AI describes / answers about it ([sequence](/diagrams/sequence-image)) |
| `"/ai วาดรูป…"` / "draw me…" | — | image generation via Cloudflare Workers AI |

Without a session, a photo gets a hint to start one first — image messages
carry no text, so the session is the only signal they're meant for the AI.
