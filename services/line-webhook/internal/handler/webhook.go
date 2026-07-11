package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/line/line-bot-sdk-go/v7/linebot"
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

	replyMessage := fmt.Sprintf("You said: %s", userMessage)

	switch userMessage {
	case "hello", "Hello", "hi", "Hi":
		replyMessage = "Hello! How can I help you today?"
	case "help", "Help":
		replyMessage = "Available commands:\n- hello: Greet the bot\n- help: Show this help message\n- Any other message will be echoed back"
	}

	_, err := h.bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(replyMessage)).Do()
	return err
}

func (h *LineHandler) handleFollowEvent(event *linebot.Event) error {
	log.Printf("User %s followed the bot", event.Source.UserID)

	welcomeMessage := "Welcome! Thank you for adding me as a friend. \n\nSend me any message and I'll echo it back to you!\n\nType 'help' to see available commands."

	_, err := h.bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(welcomeMessage)).Do()
	return err
}

func (h *LineHandler) handlePostbackEvent(event *linebot.Event) error {
	postback := event.Postback
	log.Printf("Received postback: %s from user: %s", postback.Data, event.Source.UserID)

	var response map[string]interface{}
	if err := json.Unmarshal([]byte(postback.Data), &response); err != nil {
		log.Printf("Failed to parse postback data: %v", err)
		return nil
	}

	replyMessage := fmt.Sprintf("Received postback: %v", response)
	_, err := h.bot.ReplyMessage(event.ReplyToken, linebot.NewTextMessage(replyMessage)).Do()
	return err
}
