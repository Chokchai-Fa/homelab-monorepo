# Consumer LLM Processor

This service is the AI worker in the LINE chat pipeline. It subscribes to NATS events from the webhook layer, builds a reply with Gemini using recent conversation history, and publishes the answer for delivery back to the user.

## What it does

- Consumes requests from the `line.chat.ai_request` subject
- Loads recent conversation history from Postgres with Redis caching
- Sends the prompt to Gemini and generates a response
- Publishes the final reply to `line.chat.reply`
- Supports simple conversation controls such as reset and empty-query guidance

## Runtime dependencies

- NATS for event transport
- PostgreSQL for persistent conversation history
- Redis for short-lived conversation cache
- Gemini API key for generation

## Required environment variables

```bash
NATS_URL=nats://localhost:4222
NATS_USER=
NATS_PASSWORD=
GEMINI_API_KEY=your_gemini_api_key
GEMINI_MODEL=gemini-3.5-flash
DATABASE_URL=postgres://user:password@localhost:5432/app?sslmode=disable
REDIS_ADDR=localhost:6379
REDIS_USERNAME=
REDIS_PASSWORD=
```

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
