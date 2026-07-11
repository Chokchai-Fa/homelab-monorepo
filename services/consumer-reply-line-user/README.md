# Consumer Reply Line User

This service receives AI-generated reply events from NATS and sends them to LINE users. It tries to use the original reply token first for a direct reply, and falls back to a push message if the reply token is no longer valid.

## What it does

- Subscribes to the `line.chat.reply` subject
- Receives structured reply events containing `user_id`, `reply_token`, and `text`
- Sends the message to LINE using the bot client
- Falls back to a push message when reply-token delivery fails

## Required environment variables

```bash
NATS_URL=nats://localhost:4222
NATS_USER=
NATS_PASSWORD=
LINE_CHANNEL_SECRET=your_line_channel_secret
LINE_CHANNEL_ACCESS_TOKEN=your_line_channel_access_token
```

## Local development

```bash
cd services/consumer-reply-line-user
go mod tidy
go run .
```

The service runs as a long-lived consumer process and does not expose an HTTP server.

## Message flow

1. The AI consumer publishes a reply event to NATS.
2. This service consumes the event and creates a LINE text message.
3. The message is delivered either as a reply or as a push message depending on availability.

## Notes

- The service must share the same NATS subject and queue group conventions as the producer.
- LINE credentials are required at runtime for both reply and push delivery.
