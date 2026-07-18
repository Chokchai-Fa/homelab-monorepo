# Consumer LLM Processor

This service is the AI worker in the LINE chat pipeline. It subscribes to NATS events from the webhook layer, builds a reply using recent conversation history, and publishes the answer for delivery back to the user. A difficulty router classifies each question and picks a free-tier model to answer it, falling back to the next provider on errors or rate limits.

## What it does

- Consumes requests from the `line.chat.ai_request` subject
- Loads recent conversation history from Postgres with Redis caching
- Classifies each question as simple / general / technical with a tiny classifier model
- Routes to a provider chain per tier, falling back on errors so free-tier rate limits never drop a reply:
  - simple: Gemini flash-lite → Groq → OpenRouter
  - general: Groq (Llama 70B) → Gemini → OpenRouter
  - technical: OpenRouter (reasoning model) → Groq → Gemini deep thinking → Gemini
- Publishes the final reply to `line.chat.reply`
- Supports simple conversation controls such as reset and empty-query guidance

## Runtime dependencies

- NATS for event transport
- PostgreSQL for persistent conversation history
- Redis for short-lived conversation cache
- Gemini API key for generation (required); Groq and OpenRouter keys optional — each is enabled just by setting its key, and with only Gemini configured every tier falls back to it

## Required environment variables

```bash
NATS_URL=nats://localhost:4222
NATS_USER=
NATS_PASSWORD=
GEMINI_API_KEY=your_gemini_api_key
GEMINI_MODEL=gemini-3.1-flash-lite
DATABASE_URL=postgres://user:password@localhost:5432/app?sslmode=disable
REDIS_ADDR=localhost:6379
REDIS_USERNAME=
REDIS_PASSWORD=

# Optional free-tier providers (enable by setting the API key)
GROQ_API_KEY=
GROQ_MODEL=llama-3.3-70b-versatile
GROQ_CLASSIFIER_MODEL=llama-3.1-8b-instant
OPENROUTER_API_KEY=
OPENROUTER_MODEL=deepseek/deepseek-r1:free
```

When `GROQ_API_KEY` is set, the classifier runs on the small Groq model (fast, generous free quota); otherwise Gemini classifies its own traffic.

## Local development

```bash
cd services/consumer-llm-processor
go mod tidy
go run .
```

The service runs as a long-lived consumer process and does not expose an HTTP server.

## Message flow

1. The webhook service publishes a request event with `user_id`, `reply_token`, and text.
2. This service receives the event and prepares an AI response.
3. The reply is published to the downstream LINE delivery consumer.

## Notes

- The service degrades gracefully if Redis is unavailable and continues using Postgres only.
- Conversation history is stored per user ID and can be reset with a dedicated reset command.
