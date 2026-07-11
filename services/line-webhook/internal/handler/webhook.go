package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/publisher"
)

// Webhook handles incoming LINE webhook requests
func (h *LineHandler) Webhook(c echo.Context) error {
	// request body has already been validated by middleware; parse events now

	log.Printf("Request: %v", c.Request())
	events, err := linebot.ParseRequest(h.cfg.ChannelSecret, c.Request())
	if err != nil {
		if err == linebot.ErrInvalidSignature {
			return echo.NewHTTPError(http.StatusUnauthorized, "Invalid signature")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to parse request")
	}

	for _, event := range events {
		if err := h.handleEvent(event); err != nil {
			log.Printf("Error handling event: %v", err)
		}
	}

	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
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
		log.Printf("User %s unfollowed the bot", event.Source.UserID)
	case linebot.EventTypePostback:
		return h.handlePostbackEvent(event)
	}
	return nil
}

func (h *LineHandler) handleTextMessage(event *linebot.Event, message *linebot.TextMessage) error {
	userMessage := message.Text
	log.Printf("Received text message: %s from user: %s", userMessage, event.Source.UserID)

	if h.isAIRequest(userMessage) {
		return h.handleAIRequest(event, userMessage)
	}

	replyMessage := fmt.Sprintf("You said: %s", userMessage)

	switch userMessage {
	case "hello", "Hello", "hi", "Hi":
		replyMessage = "Hello! How can I help you today?"
	case "help", "Help":
		replyMessage = "Available commands:\n- hello: Greet the bot\n- help: Show this help message\n- " + h.cfg.AIPrefix + " <question>: Ask the AI assistant (any language)\n- " + h.cfg.AIPrefix + " reset: Clear your AI conversation history\n- Any other message will be echoed back."
	}

	return h.reply(event, replyMessage)
}

// isAIRequest reports whether the message is addressed to the AI assistant:
// the prefix alone ("/ai") or followed by a question ("/ai ...").
func (h *LineHandler) isAIRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == h.cfg.AIPrefix || strings.HasPrefix(trimmed, h.cfg.AIPrefix+" ")
}

// handleAIRequest publishes the message to NATS for consumer-llm-processor,
// which answers through consumer-reply-line-user using the reply token.
func (h *LineHandler) handleAIRequest(event *linebot.Event, userMessage string) error {
	if h.pub == nil {
		log.Printf("AI request from %s dropped: NATS publisher not connected", event.Source.UserID)
		return nil
	}

	query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userMessage), h.cfg.AIPrefix))
	err := h.pub.PublishAIRequest(publisher.AIRequestEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Text:       query,
		Timestamp:  event.Timestamp.UnixMilli(),
	})
	if err == nil {
		log.Printf("Published AI request for user %s", event.Source.UserID)
		return nil
	}
	log.Printf("Failed to publish AI request for %s: %v", event.Source.UserID, err)

	unavailable := "Sorry, the AI assistant is unavailable right now. Please try again later.\nขออภัย ตอนนี้ผู้ช่วย AI ไม่พร้อมใช้งาน กรุณาลองใหม่ภายหลัง"
	return h.reply(event, unavailable)
}

func (h *LineHandler) handleFollowEvent(event *linebot.Event) error {
	log.Printf("User %s followed the bot", event.Source.UserID)

	welcomeMessage := "Welcome! Thank you for adding me as a friend. \n\nSend me any message and I'll echo it back to you!\n\nType 'help' to see available commands."

	return h.reply(event, welcomeMessage)
}

func (h *LineHandler) handlePostbackEvent(event *linebot.Event) error {
	postback := event.Postback
	log.Printf("Received postback: %s from user: %s", postback.Data, event.Source.UserID)

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(postback.Data), &response); err != nil {
		log.Printf("Failed to parse postback data: %v", err)
		return nil
	}

	return h.reply(event, fmt.Sprintf("Received postback: %v", response))
}

// reply publishes an outgoing message for consumer-reply-line-user to send.
func (h *LineHandler) reply(event *linebot.Event, text string) error {
	if h.pub == nil {
		log.Printf("Reply to %s dropped: NATS publisher not connected", event.Source.UserID)
		return nil
	}
	return h.pub.PublishReply(publisher.ReplyEvent{
		UserID:     event.Source.UserID,
		ReplyToken: event.ReplyToken,
		Text:       text,
	})
}
