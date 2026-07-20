// Package handler implements the HTTP endpoints of portfolio-chat-gateway:
// the bridge between the portfolio website's chat widget and
// consumer-llm-processor's webchat channel over NATS request-reply.
package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// RequestSubject must match consumer-llm-processor's webchat.RequestSubject.
const RequestSubject = "portfolio.chat.ai_request"

// Requester is the slice of *nats.Conn the handler needs; a nil Requester
// means NATS is (still) unavailable and requests are answered with 503.
type Requester interface {
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
}

// ChatRequest is what the website's /api/chat proxy posts here.
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ChatResponse is the success body returned to the widget.
type ChatResponse struct {
	Text string `json:"text"`
}

// errorResponse is the failure body; Error is safe to show the visitor.
type errorResponse struct {
	Error string `json:"error"`
}

// upstreamResponse mirrors consumer-llm-processor's webchat.Response.
type upstreamResponse struct {
	Text  string `json:"text"`
	Error string `json:"error"`
}

// sessionIDPattern accepts UUIDs and similar opaque browser-generated IDs
// while rejecting anything that could smuggle structure into the store key.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Handler serves the chat API.
type Handler struct {
	nc       Requester
	timeout  time.Duration
	maxChars int
}

// New creates the handler. nc may be nil (NATS down at startup); requests
// then fail with 503 until the connection is set.
func New(nc Requester, timeout time.Duration, maxChars int) *Handler {
	return &Handler{nc: nc, timeout: timeout, maxChars: maxChars}
}

// Healthz reports liveness; the gateway is healthy even while NATS
// reconnects (requests degrade to 503).
func (h *Handler) Healthz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Chat validates one widget message and relays it to
// consumer-llm-processor via NATS request-reply.
func (h *Handler) Chat(c echo.Context) error {
	var req ChatRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request body"})
	}
	if !sessionIDPattern.MatchString(req.SessionID) {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid session_id"})
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "message is empty"})
	}
	if len([]rune(message)) > h.maxChars {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "message too long"})
	}
	if h.nc == nil {
		return c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "chat is temporarily unavailable"})
	}

	payload, err := json.Marshal(map[string]any{
		"session_id": req.SessionID,
		"message":    message,
		"timestamp":  time.Now().Unix(),
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}

	start := time.Now()
	msg, err := h.nc.Request(RequestSubject, payload, h.timeout)
	if err != nil {
		log.Error().Str("subject", RequestSubject).Err(err).Msg("chat: NATS request failed")
		if err == nats.ErrTimeout {
			return c.JSON(http.StatusGatewayTimeout, errorResponse{Error: "the assistant took too long to answer, please try again"})
		}
		return c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "chat is temporarily unavailable"})
	}

	var resp upstreamResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		log.Error().Err(err).Msg("chat: invalid upstream response")
		return c.JSON(http.StatusBadGateway, errorResponse{Error: "chat is temporarily unavailable"})
	}
	if resp.Error != "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: resp.Error})
	}
	log.Info().Dur("duration", time.Since(start)).Int("answer_chars", len(resp.Text)).Msg("chat: answered")
	return c.JSON(http.StatusOK, ChatResponse{Text: resp.Text})
}
