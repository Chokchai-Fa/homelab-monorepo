# Consumer LLM Processor

This service is the AI worker in the LINE chat pipeline. It subscribes to NATS events from the webhook layer, builds a reply using recent conversation history, and publishes the answer for delivery back to the user. A difficulty router classifies each question and picks a free-tier model to answer it, falling back to the next provider on errors or rate limits.

## What it does

- Consumes requests from the `line.chat.ai_request` subject
- Debounces each user's burst of rapid chat messages (people type "hey" / "quick question" / "how do I..." as separate messages): buffers until the user has been quiet for `DEBOUNCE_WINDOW`, then answers the merged burst as one LLM request using the newest reply token
- Loads recent conversation history from Postgres with Redis caching
- Classifies each question as simple / general / technical with a tiny classifier model
- Routes to a provider chain per tier, falling back on errors so free-tier rate limits never drop a reply:
  - simple: Gemini flash-lite → Groq → OpenRouter
  - general: Groq (Llama 70B) → Gemini → OpenRouter
  - technical: OpenRouter (reasoning model) → Groq → Gemini deep thinking → Gemini
  - vision (an image is attached): Gemini → OpenRouter vision model — difficulty tiering is skipped, since matching a model that can actually see the image matters more than matching difficulty
- Fetches image bytes line-webhook stashed in Redis (`internal/imagecache`) when the request carries an `image_key`, deletes them once read, and stores only a text placeholder in conversation history
- Publishes the final reply to `line.chat.reply`
- Supports simple conversation controls such as `/reset` and empty-query guidance

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
OPENROUTER_MODEL=nvidia/nemotron-3-ultra-550b-a55b:free
OPENROUTER_VISION_MODEL=google/gemma-4-31b-it:free

# Chat debouncing: answer a burst of rapid messages as one request after the
# user has been quiet for DEBOUNCE_WINDOW (0 answers each message alone);
# DEBOUNCE_MAX_WAIT caps buffering for someone who never stops typing.
DEBOUNCE_WINDOW=5s
DEBOUNCE_MAX_WAIT=15s
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

1. The webhook service publishes a request event with `user_id`, `reply_token`, and text (or, for an image sent during an AI session, an `image_key`/`image_mime` pointing at bytes it stashed in Redis).
2. This service receives the event, fetches the image from Redis if present, and prepares an AI response.
3. The reply is published to the downstream LINE delivery consumer.

## Notes

- The service degrades gracefully if Redis is unavailable and continues using Postgres only. Image requests specifically require Redis; without it, images are declined with a message.
- Conversation history is stored per user ID and can be cleared with the `/reset` command.
- Groq's free vision model (Llama 4 Scout) was deprecated in June 2026, so Groq never sees images - only Gemini and OpenRouter's vision model are wired for that.
