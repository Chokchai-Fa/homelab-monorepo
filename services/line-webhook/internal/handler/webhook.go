package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/rs/zerolog/log"

	"line-webhook/internal/publisher"
)

// Webhook handles incoming LINE webhook requests
func (h *LineHandler) Webhook(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		log.Error().Err(err).Msg("webhook: failed to read request body")
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to read request body")
	}
	c.Request().Body = io.NopCloser(bytes.NewReader(body))

	// request body has already been validated by middleware; parse events now
	events, err := linebot.ParseRequest(h.cfg.ChannelSecret, c.Request())
	if err != nil {
		if err == linebot.ErrInvalidSignature {
			log.Error().Err(err).Msg("webhook: invalid signature")
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid signature")
		}
		log.Error().Err(err).Msg("webhook: failed to parse request")
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to parse request")
	}
	log.Info().Int("events", len(events)).Msg("webhook: request parsed")
	log.Debug().RawJSON("payload", body).Msg("webhook: raw payload")

	for i, event := range events {
		if err := h.markAsRead(body, i); err != nil {
			log.Warn().Str("type", string(event.Type)).Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to mark message as read")
		}
		if err := h.handleEvent(event); err != nil {
			log.Error().Str("type", string(event.Type)).Str("user_id", event.Source.UserID).Err(err).Msg("webhook: event handling failed")
		}
	}

	return c.NoContent(http.StatusOK)
}

func (h *LineHandler) markAsRead(body []byte, index int) error {
	if len(body) == 0 || index < 0 || h.cfg == nil || h.cfg.ChannelToken == "" {
		return nil
	}

	token, err := extractMarkAsReadToken(body, index)
	if err != nil {
		log.Error().Int("index", index).Err(err).Msg("markAsRead: failed to extract markAsReadToken")
		return err
	}
	if token == "" {
		log.Warn().Int("index", index).Msg("markAsRead: markAsReadToken is empty - LINE may not be sending it; check LINE OA chat feature settings")
		return nil
	}
	log.Debug().Int("index", index).Str("token_prefix", token[:min(len(token), 8)]+"...").Msg("markAsRead: extracted token")

	payload := map[string]string{"markAsReadToken": token}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.line.me/v2/bot/chat/markAsRead", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.cfg.ChannelToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error().Int("index", index).Err(err).Msg("markAsRead: HTTP request failed")
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Error().Int("index", index).Int("status", resp.StatusCode).Str("body", string(respBody)).Msg("markAsRead: LINE API returned error")
		return fmt.Errorf("mark as read failed with status %d", resp.StatusCode)
	}
	log.Debug().Int("index", index).Int("status", resp.StatusCode).Msg("markAsRead: success")
	return nil
}

func extractMarkAsReadToken(body []byte, index int) (string, error) {
	var payload struct {
		Events []struct {
			Message struct {
				MarkAsReadToken string `json:"markAsReadToken"`
			} `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if index < 0 || index >= len(payload.Events) {
		return "", nil
	}
	return payload.Events[index].Message.MarkAsReadToken, nil
}

func (h *LineHandler) handleEvent(event *linebot.Event) error {
	switch event.Type {
	case linebot.EventTypeMessage:
		switch message := event.Message.(type) {
		case *linebot.TextMessage:
			return h.handleTextMessage(event, message)
		case *linebot.ImageMessage:
			return h.handleImageMessage(event, message)
		}
	case linebot.EventTypeFollow:
		return h.handleFollowEvent(event)
	case linebot.EventTypeUnfollow:
		log.Info().Str("user_id", event.Source.UserID).Msg("webhook: user unfollowed")
	case linebot.EventTypePostback:
		return h.handlePostbackEvent(event)
	}
	return nil
}

func (h *LineHandler) handleTextMessage(event *linebot.Event, message *linebot.TextMessage) error {
	userMessage := message.Text
	trimmed := strings.TrimSpace(userMessage)
	log.Info().Str("user_id", event.Source.UserID).Str("text", userMessage).Msg("webhook: text message received")
	h.ensureProfile(event.Source.UserID)

	// AI session lifecycle: "/ai" starts a session (messages flow to the AI
	// without any prefix), "/ai-end" ends it, and it auto-expires after the
	// session TTL of inactivity.
	if trimmed == h.cfg.AIPrefix+"-end" {
		return h.handleAIEnd(event)
	}
	if h.isAIRequest(userMessage) {
		return h.handleAIStart(event, trimmed)
	}
	// Reminder trigger keywords reach the AI pipeline even outside an AI
	// session: consumer-llm-processor detects the intent, extracts the
	// details and hands off to consumer-reminder. No AI session is started.
	if isReminderRequest(trimmed) {
		log.Info().Str("user_id", event.Source.UserID).Msg("webhook: reminder keyword - forwarding to AI pipeline")
		return h.publishAIRequest(event, trimmed)
	}
	// Mid-flow answers (free text while consumer-reminder's conversation is
	// open) also go to the AI pipeline. The flow key is owned and expired by
	// consumer-reminder, so this routing dies with the flow and never leaves
	// the user in AI mode afterwards.
	if h.sessions != nil && event.Source.UserID != "" && h.sessions.FlowActive(context.Background(), event.Source.UserID) {
		log.Info().Str("user_id", event.Source.UserID).Msg("webhook: reminder flow active - routing message to AI pipeline")
		return h.publishAIRequest(event, trimmed)
	}
	if h.sessions != nil && event.Source.UserID != "" && h.sessions.Active(context.Background(), event.Source.UserID) {
		log.Info().Str("user_id", event.Source.UserID).Msg("webhook: active AI session - routing message to AI")
		return h.publishAIRequest(event, trimmed)
	}

	replyMessage := fmt.Sprintf("You said: %s", userMessage)

	switch userMessage {
	case "hello", "Hello", "hi", "Hi":
		replyMessage = "Hello! How can I help you today?"
	case "help", "Help":
		replyMessage = "Available commands:\n- hello: Greet the bot\n- help: Show this help message\n- " + h.cfg.AIPrefix + " <question>: Start an AI session and ask (any language). While the session is active, every message goes to the AI.\n- " + h.cfg.AIPrefix + "-end: End the AI session (it also ends after 10 minutes of silence)\n- " + h.cfg.AIPrefix + " /reset: Clear your AI conversation history\n- Any other message will be echoed back."
	}

	return h.reply(event, replyMessage)
}

// isAIRequest reports whether the message is addressed to the AI assistant:
// the prefix alone ("/ai") or followed by a question ("/ai ...").
func (h *LineHandler) isAIRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == h.cfg.AIPrefix || strings.HasPrefix(trimmed, h.cfg.AIPrefix+" ")
}

// isReminderRequest reports whether the message starts a reminder: the hard
// keyword "/reminder" (alone or with details) or Thai "ตั้งเตือน..." (no
// space needed - Thai doesn't use them). Must match consumer-llm-processor's
// view of a trigger.
func isReminderRequest(trimmed string) bool {
	return trimmed == "/reminder" ||
		strings.HasPrefix(trimmed, "/reminder ") ||
		strings.HasPrefix(trimmed, "ตั้งเตือน")
}

// ensureProfile fetches and publishes the user's LINE profile at most once
// per gate TTL, so consumer-reminder can keep the line_users table fresh.
// Runs in the background - profile upkeep must never delay a reply.
func (h *LineHandler) ensureProfile(userID string) {
	if userID == "" || h.profiles == nil || h.bot == nil || h.pub == nil {
		return
	}
	if !h.profiles.TryClaim(context.Background(), userID) {
		return
	}
	go func() {
		profile, err := h.bot.GetProfile(userID).Do()
		if err != nil {
			log.Error().Str("user_id", userID).Err(err).Msg("webhook: profile fetch failed - releasing gate for retry")
			h.profiles.Release(context.Background(), userID)
			return
		}
		if err := h.pub.PublishProfile(publisher.ProfileEvent{
			UserID:      userID,
			DisplayName: profile.DisplayName,
			Timestamp:   time.Now().UnixMilli(),
		}); err != nil {
			log.Error().Str("user_id", userID).Err(err).Msg("webhook: failed to publish profile - releasing gate for retry")
			h.profiles.Release(context.Background(), userID)
			return
		}
		log.Info().Str("user_id", userID).Str("display_name", profile.DisplayName).Msg("webhook: profile published")
	}()
}

// handleAIStart opens (or refreshes) the user's AI session and forwards the
// question, if any, to consumer-llm-processor.
func (h *LineHandler) handleAIStart(event *linebot.Event, trimmed string) error {
	if h.sessions != nil && event.Source.UserID != "" {
		if err := h.sessions.Start(context.Background(), event.Source.UserID); err != nil {
			log.Error().Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to start AI session - prefix still works per message")
		} else {
			log.Info().Str("user_id", event.Source.UserID).Msg("webhook: AI session started")
		}
	}

	query := strings.TrimSpace(strings.TrimPrefix(trimmed, h.cfg.AIPrefix))
	if query == "" {
		return h.reply(event, "AI session started! Just type your messages - no prefix needed.\n\nType "+h.cfg.AIPrefix+"-end to stop (auto-ends after 10 minutes of silence).\n\nเริ่มคุยกับ AI ได้เลย พิมพ์ข้อความได้ตามปกติ ไม่ต้องใส่ "+h.cfg.AIPrefix+" แล้วน้า~")
	}
	return h.publishAIRequest(event, query)
}

// handleAIEnd closes the user's AI session.
func (h *LineHandler) handleAIEnd(event *linebot.Event) error {
	if h.sessions != nil && event.Source.UserID != "" {
		if err := h.sessions.End(context.Background(), event.Source.UserID); err != nil {
			log.Error().Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to end AI session")
			return h.reply(event, "Sorry, I couldn't end the AI session. Please try again.")
		}
		log.Info().Str("user_id", event.Source.UserID).Msg("webhook: AI session ended")
	}
	return h.reply(event, "AI session ended. Type "+h.cfg.AIPrefix+" to start a new one.\nจบการคุยกับ AI แล้ว พิมพ์ "+h.cfg.AIPrefix+" เพื่อเริ่มใหม่ได้เลย")
}

// publishAIRequest sends the question to NATS for consumer-llm-processor,
// which answers through consumer-reply-line-user using the reply token.
func (h *LineHandler) publishAIRequest(event *linebot.Event, query string) error {
	return h.publishAIRequestWithImage(event, query, "", "")
}

func (h *LineHandler) publishAIRequestWithImage(event *linebot.Event, query, imageKey, imageMime string) error {
	if h.pub == nil {
		log.Error().Str("user_id", event.Source.UserID).Msg("webhook: AI request dropped - NATS publisher not connected")
		return nil
	}

	err := h.pub.PublishAIRequest(publisher.AIRequestEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Text:       query,
		ImageKey:   imageKey,
		ImageMime:  imageMime,
		Timestamp:  event.Timestamp.UnixMilli(),
	})
	if err == nil {
		log.Info().Str("subject", publisher.AIRequestSubject).Str("user_id", event.Source.UserID).Msg("webhook: AI request published")
		return nil
	}
	log.Error().Str("subject", publisher.AIRequestSubject).Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to publish AI request")

	unavailable := "Sorry, the AI assistant is unavailable right now. Please try again later.\nขออภัย ตอนนี้ผู้ช่วย AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง"
	return h.reply(event, unavailable)
}

// maxImageBytes falls back to 10MB (comfortably under LINE's own image
// upload limit and the free vision providers' per-request caps) if unset.
const defaultMaxImageBytes = 10 << 20

// handleImageMessage forwards an image the user sent to the AI, but only
// while an AI session is active: LINE image messages carry no caption text,
// so there's no other signal that the user wants the AI to look at it.
func (h *LineHandler) handleImageMessage(event *linebot.Event, message *linebot.ImageMessage) error {
	userID := event.Source.UserID
	log.Info().Str("user_id", userID).Str("message_id", message.ID).Msg("webhook: image message received")
	h.ensureProfile(userID)

	if h.sessions == nil || userID == "" || !h.sessions.Active(context.Background(), userID) {
		return h.reply(event, "Start an AI session first with "+h.cfg.AIPrefix+", then send your image.\nเริ่มคุยกับ AI ด้วย "+h.cfg.AIPrefix+" ก่อน แล้วค่อยส่งรูปมาน้า~")
	}
	if h.bot == nil || h.images == nil {
		log.Error().Str("user_id", userID).Msg("webhook: image dropped - LINE client or image store not configured")
		return h.reply(event, "Sorry, I can't process images right now. Please try again later.")
	}

	content, err := h.bot.GetMessageContent(message.ID).Do()
	if err != nil {
		log.Error().Str("user_id", userID).Str("message_id", message.ID).Err(err).Msg("webhook: failed to download image content")
		return h.reply(event, "Sorry, I couldn't download that image. Please try again.")
	}
	defer content.Content.Close()

	maxBytes := h.cfg.MaxImageBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxImageBytes
	}
	data, err := io.ReadAll(io.LimitReader(content.Content, maxBytes+1))
	if err != nil {
		log.Error().Str("user_id", userID).Str("message_id", message.ID).Err(err).Msg("webhook: failed to read image content")
		return h.reply(event, "Sorry, I couldn't read that image. Please try again.")
	}
	if int64(len(data)) > maxBytes {
		log.Warn().Str("user_id", userID).Str("message_id", message.ID).Int("bytes", len(data)).Msg("webhook: image exceeds size limit - dropping")
		return h.reply(event, "That image is too large for me to look at. Please send a smaller one.")
	}

	ttl := h.cfg.ImageTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if err := h.images.Put(context.Background(), message.ID, data, ttl); err != nil {
		log.Error().Str("user_id", userID).Str("message_id", message.ID).Err(err).Msg("webhook: failed to stash image - dropping")
		return h.reply(event, "Sorry, I couldn't process that image. Please try again.")
	}

	mime := content.ContentType
	if mime == "" {
		mime = "image/jpeg"
	}
	return h.publishAIRequestWithImage(event, "", message.ID, mime)
}

func (h *LineHandler) handleFollowEvent(event *linebot.Event) error {
	log.Info().Str("user_id", event.Source.UserID).Msg("webhook: user followed")
	h.ensureProfile(event.Source.UserID)

	welcomeMessage := "Welcome! Thank you for adding me as a friend. \n\nSend me any message and I'll echo it back to you!\n\nType 'help' to see available commands."

	return h.reply(event, welcomeMessage)
}

// handlePostbackEvent forwards the raw postback payload to NATS for
// consumer-reminder; the webhook does no parsing of its own.
func (h *LineHandler) handlePostbackEvent(event *linebot.Event) error {
	postback := event.Postback
	log.Info().Str("user_id", event.Source.UserID).Str("data", postback.Data).Msg("webhook: postback received")
	h.ensureProfile(event.Source.UserID)

	if h.pub == nil {
		log.Error().Str("user_id", event.Source.UserID).Msg("webhook: postback dropped - NATS publisher not connected")
		return nil
	}
	if err := h.pub.PublishPostback(publisher.PostbackEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Data:       postback.Data,
		Timestamp:  event.Timestamp.UnixMilli(),
	}); err != nil {
		log.Error().Str("subject", publisher.PostbackSubject).Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to publish postback")
		return err
	}
	log.Info().Str("subject", publisher.PostbackSubject).Str("user_id", event.Source.UserID).Msg("webhook: postback published")
	return nil
}

// reply publishes an outgoing message for consumer-reply-line-user to send.
func (h *LineHandler) reply(event *linebot.Event, text string) error {
	if h.pub == nil {
		log.Error().Str("user_id", event.Source.UserID).Msg("webhook: reply dropped - NATS publisher not connected")
		return nil
	}
	if err := h.pub.PublishReply(publisher.ReplyEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Text:       text,
	}); err != nil {
		log.Error().Str("subject", publisher.ReplySubject).Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to publish reply")
		return err
	}
	log.Info().Str("subject", publisher.ReplySubject).Str("user_id", event.Source.UserID).Msg("webhook: reply published")
	return nil
}
