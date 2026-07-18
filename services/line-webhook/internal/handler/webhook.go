package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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

	// AI session lifecycle: "/ai" starts a session (messages flow to the AI
	// without any prefix), "/ai-end" ends it, and it auto-expires after the
	// session TTL of inactivity.
	if trimmed == h.cfg.AIPrefix+"-end" {
		return h.handleAIEnd(event)
	}
	if h.isAIRequest(userMessage) {
		return h.handleAIStart(event, trimmed)
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
		replyMessage = "Available commands:\n- hello: Greet the bot\n- help: Show this help message\n- " + h.cfg.AIPrefix + " <question>: Start an AI session and ask (any language). While the session is active, every message goes to the AI.\n- " + h.cfg.AIPrefix + "-end: End the AI session (it also ends after 10 minutes of silence)\n- " + h.cfg.AIPrefix + " reset: Clear your AI conversation history\n- Any other message will be echoed back."
	}

	return h.reply(event, replyMessage)
}

// isAIRequest reports whether the message is addressed to the AI assistant:
// the prefix alone ("/ai") or followed by a question ("/ai ...").
func (h *LineHandler) isAIRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == h.cfg.AIPrefix || strings.HasPrefix(trimmed, h.cfg.AIPrefix+" ")
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
	if h.pub == nil {
		log.Error().Str("user_id", event.Source.UserID).Msg("webhook: AI request dropped - NATS publisher not connected")
		return nil
	}

	err := h.pub.PublishAIRequest(publisher.AIRequestEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Text:       query,
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

func (h *LineHandler) handleFollowEvent(event *linebot.Event) error {
	log.Info().Str("user_id", event.Source.UserID).Msg("webhook: user followed")

	welcomeMessage := "Welcome! Thank you for adding me as a friend. \n\nSend me any message and I'll echo it back to you!\n\nType 'help' to see available commands."

	return h.reply(event, welcomeMessage)
}

func (h *LineHandler) handlePostbackEvent(event *linebot.Event) error {
	postback := event.Postback
	log.Info().Str("user_id", event.Source.UserID).Str("data", postback.Data).Msg("webhook: postback received")

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(postback.Data), &response); err != nil {
		log.Error().Str("user_id", event.Source.UserID).Err(err).Msg("webhook: failed to parse postback data")
		return nil
	}

	return h.reply(event, fmt.Sprintf("Received postback: %v", response))
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
