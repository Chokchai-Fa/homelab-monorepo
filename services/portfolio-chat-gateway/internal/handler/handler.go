// Package handler implements the HTTP endpoints of portfolio-chat-gateway:
// the bridge between the portfolio website's chat widget and
// consumer-llm-processor's webchat channel over NATS. It offers a unary
// request-reply endpoint (POST /chat) and a streaming SSE endpoint
// (POST /chat/stream), plus a live homelab status endpoint.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

const (
	// RequestSubject must match consumer-llm-processor's webchat.RequestSubject.
	RequestSubject = "portfolio.chat.ai_request"
	// RequestStreamSubject must match webchat.RequestStreamSubject.
	RequestStreamSubject = "portfolio.chat.ai_request.stream"
)

// StreamSub delivers the reply frames of one streaming request. Next blocks
// up to timeout for the next raw frame; Close releases the subscription.
type StreamSub interface {
	Next(timeout time.Duration) ([]byte, error)
	Close() error
}

// Backend is the gateway's NATS dependency. A nil Backend means NATS is
// unavailable and requests degrade to 503.
type Backend interface {
	// Request does one unary request-reply round trip.
	Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error)
	// OpenStream publishes payload with a fresh reply inbox and returns a
	// subscription delivering the reply frames.
	OpenStream(subject string, payload []byte) (StreamSub, error)
	// Connected reports whether the NATS connection is currently up.
	Connected() bool
}

// ChatRequest is what the website's /api/chat proxy posts here.
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ChatResponse is the success body returned to the widget (unary path).
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

// streamFrame mirrors webchat.StreamChunk: a delta, or the terminating
// {done:true} frame (with Error set when generation failed).
type streamFrame struct {
	Delta string `json:"delta,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

// StatusResponse powers the widget's "answered from my homelab" card.
type StatusResponse struct {
	Status string `json:"status"`
	NATS   bool   `json:"nats"`
	Uptime string `json:"uptime"`
	Host   string `json:"host"`
}

// sessionIDPattern accepts UUIDs and similar opaque browser-generated IDs
// while rejecting anything that could smuggle structure into the store key.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Handler serves the chat API.
type Handler struct {
	backend  Backend
	timeout  time.Duration
	maxChars int
	start    time.Time
	host     string
}

// New creates the handler. backend may be nil (NATS down at startup); requests
// then fail with 503 until a connection exists.
func New(backend Backend, timeout time.Duration, maxChars int) *Handler {
	host, _ := os.Hostname()
	return &Handler{backend: backend, timeout: timeout, maxChars: maxChars, start: time.Now(), host: host}
}

// Healthz reports liveness; the gateway is healthy even while NATS reconnects
// (chat requests degrade to 503 meanwhile).
func (h *Handler) Healthz(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// Status reports live homelab health for the widget's status card.
func (h *Handler) Status(c echo.Context) error {
	return c.JSON(http.StatusOK, StatusResponse{
		Status: "ok",
		NATS:   h.backend != nil && h.backend.Connected(),
		Uptime: time.Since(h.start).Round(time.Second).String(),
		Host:   h.host,
	})
}

// validate parses and checks a chat request, returning the trimmed message or
// an HTTP status + reason to reject with.
func (h *Handler) validate(c echo.Context) (req ChatRequest, message string, status int, reason string) {
	if err := c.Bind(&req); err != nil {
		return req, "", http.StatusBadRequest, "invalid request body"
	}
	if !sessionIDPattern.MatchString(req.SessionID) {
		return req, "", http.StatusBadRequest, "invalid session_id"
	}
	message = strings.TrimSpace(req.Message)
	if message == "" {
		return req, "", http.StatusBadRequest, "message is empty"
	}
	if len([]rune(message)) > h.maxChars {
		return req, "", http.StatusBadRequest, "message too long"
	}
	return req, message, 0, ""
}

func (h *Handler) payload(req ChatRequest, message string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"session_id": req.SessionID,
		"message":    message,
		"timestamp":  time.Now().Unix(),
	})
}

// Chat validates one widget message and relays it to consumer-llm-processor
// via NATS request-reply, returning the whole answer as JSON.
func (h *Handler) Chat(c echo.Context) error {
	req, message, status, reason := h.validate(c)
	if status != 0 {
		return c.JSON(status, errorResponse{Error: reason})
	}
	if h.backend == nil {
		return c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "chat is temporarily unavailable"})
	}

	payload, err := h.payload(req, message)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}

	start := time.Now()
	msg, err := h.backend.Request(RequestSubject, payload, h.timeout)
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

// ChatStream is the streaming counterpart of Chat: it relays the message over
// NATS and forwards the consumer's StreamChunk frames to the browser as
// Server-Sent Events, so the answer renders token-by-token. Errors before the
// stream opens are returned as normal JSON status codes; once the SSE stream
// has started, failures are delivered as a terminating error frame.
func (h *Handler) ChatStream(c echo.Context) error {
	req, message, status, reason := h.validate(c)
	if status != 0 {
		return c.JSON(status, errorResponse{Error: reason})
	}
	if h.backend == nil {
		return c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "chat is temporarily unavailable"})
	}

	payload, err := h.payload(req, message)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal error"})
	}

	sub, err := h.backend.OpenStream(RequestStreamSubject, payload)
	if err != nil {
		log.Error().Err(err).Msg("chat-stream: failed to open stream")
		return c.JSON(http.StatusServiceUnavailable, errorResponse{Error: "chat is temporarily unavailable"})
	}
	defer sub.Close()

	w := c.Response()
	h.setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	w.Flush()

	reqCtx := c.Request().Context()
	deadline := time.Now().Add(h.timeout)
	start := time.Now()
	for {
		select {
		case <-reqCtx.Done():
			// Visitor navigated away or closed the widget.
			log.Info().Msg("chat-stream: client disconnected")
			return nil
		default:
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			h.writeSSE(w, streamFrame{Done: true, Error: "timeout"})
			log.Warn().Msg("chat-stream: overall timeout")
			return nil
		}
		idle := remaining
		if idle > 25*time.Second {
			idle = 25 * time.Second
		}

		raw, err := sub.Next(idle)
		if err != nil {
			h.writeSSE(w, streamFrame{Done: true, Error: "timeout"})
			log.Warn().Err(err).Msg("chat-stream: no frame before idle timeout")
			return nil
		}

		// Forward the frame verbatim (it is already the {delta}/{done} shape
		// the widget expects) and stop when the consumer signals done.
		var frame streamFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			log.Error().Err(err).Msg("chat-stream: bad frame from consumer")
			continue
		}
		w.Write([]byte("data: "))
		w.Write(raw)
		w.Write([]byte("\n\n"))
		w.Flush()
		if frame.Done {
			log.Info().Dur("duration", time.Since(start)).Msg("chat-stream: completed")
			return nil
		}
	}
}

func (h *Handler) setSSEHeaders(w *echo.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering so frames reach the browser immediately.
	w.Header().Set("X-Accel-Buffering", "no")
}

func (h *Handler) writeSSE(w *echo.Response, frame streamFrame) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	w.Flush()
}
