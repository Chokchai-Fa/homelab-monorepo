// Package webchat serves the portfolio website's chat channel. Unlike the
// LINE pipeline (fire-and-forget publish, reply delivered by a downstream
// consumer), the web visitor is waiting on an open HTTP request, so this
// channel uses NATS request-reply: portfolio-chat-gateway sends a request on
// RequestSubject and this consumer answers with msg.Respond. The LINE-shaped
// features - debouncing, reminder handoff, image input/generation - are
// deliberately absent here.
package webchat

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"consumer-llm-processor/internal/ai"
	"consumer-llm-processor/internal/store"
)

const (
	// RequestSubject is the request-reply subject portfolio-chat-gateway
	// calls with nc.Request for a single, whole answer.
	RequestSubject = "portfolio.chat.ai_request"
	QueueGroup     = "consumer-llm-processor-web"

	// RequestStreamSubject is the streaming subject: the gateway publishes a
	// request with a reply inbox and this service pushes a series of
	// StreamChunk frames to that inbox, ending with one {done:true}.
	RequestStreamSubject = "portfolio.chat.ai_request.stream"
	StreamQueueGroup     = "consumer-llm-processor-web-stream"

	// userIDPrefix namespaces web sessions inside the shared conversation
	// store so they can never collide with LINE user IDs.
	userIDPrefix = "web:"

	// generateTimeout stays under the gateway's request timeout so a slow
	// provider chain still gets a friendly failure answer back to the
	// visitor instead of a gateway timeout.
	generateTimeout = 55 * time.Second

	// maxMessageChars mirrors the gateway's cap; enforced here too so the
	// consumer never trusts the edge alone.
	maxMessageChars = 2000
)

// Request is what portfolio-chat-gateway sends. SessionID is a browser
// generated UUID; history is stored under "web:<session_id>".
type Request struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// Response is the request-reply answer. Error is set only for invalid
// requests; LLM failures come back as friendly Text so the widget can show
// them verbatim.
type Response struct {
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// Responder answers one question with conversation context; satisfied by
// *ai.Router built with the portfolio persona. Route returns the whole answer;
// RouteStream emits it token-by-token through emit and returns the full text.
type Responder interface {
	Route(ctx context.Context, history []store.Message, userMessage string, image *ai.Image) (ai.Result, error)
	RouteStream(ctx context.Context, history []store.Message, userMessage string, emit func(delta string) error) (ai.Result, error)
}

// StreamChunk is one frame the gateway receives on its reply inbox. A stream
// is a sequence of {delta} frames terminated by a single {done:true}; Error
// is set on the terminating frame when generation failed.
type StreamChunk struct {
	Delta string `json:"delta,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

// ContextRetriever supplies retrieval-augmented context for a question: a
// block of relevant facts to prepend to the prompt, or "" when RAG is disabled
// or nothing relevant is found. Retrieval is best-effort, so it never returns
// an error - a failure just yields "". Satisfied by *knowledge.Retriever.
type ContextRetriever interface {
	RetrieveContext(ctx context.Context, query string) string
}

// Consumer answers portfolio chat requests over NATS request-reply.
type Consumer struct {
	store     store.Store
	ai        Responder
	nc        *nats.Conn
	retriever ContextRetriever // nil disables RAG
}

// New creates the web chat consumer. retriever may be nil, in which case the
// chat answers from the persona's prompt-embedded facts alone (no RAG).
func New(s store.Store, r Responder, nc *nats.Conn, retriever ContextRetriever) *Consumer {
	return &Consumer{store: s, ai: r, nc: nc, retriever: retriever}
}

// augment prepends retrieved context to the message sent to the LLM, without
// changing what gets stored in history (that stays the raw question). Returns
// the query unchanged when RAG is off or nothing relevant is retrieved.
func (c *Consumer) augment(ctx context.Context, query string) string {
	if c.retriever == nil {
		return query
	}
	block := c.retriever.RetrieveContext(ctx, query)
	if block == "" {
		return query
	}
	return block + "\n\nQuestion: " + query
}

// Subscribe attaches the consumer as a queue subscriber. Each request is
// answered on its own goroutine: LLM calls take seconds, and one slow
// question must not serialize every other visitor.
func (c *Consumer) Subscribe() (*nats.Subscription, error) {
	return c.nc.QueueSubscribe(RequestSubject, QueueGroup, func(msg *nats.Msg) {
		if msg.Reply == "" {
			log.Error().Str("subject", RequestSubject).Msg("webchat: dropping request without reply subject")
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
			defer cancel()
			resp := c.respond(ctx, msg.Data)
			data, err := json.Marshal(resp)
			if err != nil {
				log.Error().Err(err).Msg("webchat: failed to marshal response")
				return
			}
			if err := msg.Respond(data); err != nil {
				log.Error().Err(err).Msg("webchat: failed to respond")
			}
		}()
	})
}

// SubscribeStream attaches the streaming consumer. Each request is answered on
// its own goroutine, pushing StreamChunk frames to the request's reply inbox.
func (c *Consumer) SubscribeStream() (*nats.Subscription, error) {
	return c.nc.QueueSubscribe(RequestStreamSubject, StreamQueueGroup, func(msg *nats.Msg) {
		if msg.Reply == "" {
			log.Error().Str("subject", RequestStreamSubject).Msg("webchat: dropping stream request without reply inbox")
			return
		}
		go c.handleStream(msg)
	})
}

// handleStream wires one NATS streaming request into streamRespond: emit
// publishes a delta frame to the reply inbox, done publishes the terminating
// frame.
func (c *Consumer) handleStream(msg *nats.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
	defer cancel()

	emit := func(delta string) error {
		return c.publishChunk(msg.Reply, StreamChunk{Delta: delta})
	}
	done := func(errText string) {
		_ = c.publishChunk(msg.Reply, StreamChunk{Done: true, Error: errText})
	}
	c.streamRespond(ctx, msg.Data, emit, done)
}

// streamRespond is the transport-independent streaming logic: it validates the
// raw request, streams the answer through emit as it is generated, and always
// finishes by calling done exactly once (with a non-empty errText when
// generation failed). Kept free of NATS so it can be unit-tested directly.
func (c *Consumer) streamRespond(ctx context.Context, data []byte, emit func(delta string) error, done func(errText string)) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Error().Err(err).Msg("webchat: failed to unmarshal stream request")
		_ = emit("Sorry, I couldn't read that request.")
		done("invalid request")
		return
	}
	query := strings.TrimSpace(req.Message)
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" || query == "" {
		_ = emit(usageHint)
		done("")
		return
	}
	if len([]rune(query)) > maxMessageChars {
		_ = emit("That message is a bit too long - could you shorten it?")
		done("message too long")
		return
	}

	userID := userIDPrefix + sessionID
	log.Info().Str("user_id", userID).Int("chars", len(query)).Msg("webchat: stream request received")

	if isResetCommand(query) {
		if err := c.store.Clear(ctx, userID); err != nil {
			log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to clear history")
			_ = emit("Sorry, I couldn't reset the conversation. Please try again.")
			done("clear failed")
			return
		}
		_ = emit(resetDone)
		done("")
		return
	}

	history, err := c.store.GetRecent(ctx, userID)
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to load history - continuing without context")
		history = nil
	}

	start := time.Now()
	result, err := c.ai.RouteStream(ctx, history, c.augment(ctx, query), emit)
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: stream llm request failed")
		// If nothing was streamed yet the visitor has a blank bubble; give
		// them the friendly fallback. (If a partial answer already streamed,
		// we just end the stream - the error rides on the done frame.)
		if result.Text == "" {
			_ = emit(unavailableText)
		}
		done("llm failed")
		return
	}
	if result.ReminderIntent || result.ImageData != nil {
		_ = emit(offTopicText)
		done("")
		return
	}
	log.Info().Str("user_id", userID).Dur("duration", time.Since(start)).Int("answer_chars", len(result.Text)).Msg("webchat: stream answered")

	if err := c.store.Append(ctx, userID, store.RoleUser, query); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to store user message")
	}
	if err := c.store.Append(ctx, userID, store.RoleModel, result.Text); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to store model reply")
	}
	done("")
}

// publishChunk marshals and publishes one StreamChunk frame to the reply inbox.
func (c *Consumer) publishChunk(inbox string, chunk StreamChunk) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		log.Error().Err(err).Msg("webchat: failed to marshal stream chunk")
		return err
	}
	return c.nc.Publish(inbox, data)
}

const (
	usageHint = "Hi! I'm Chokchai's portfolio assistant. Ask me anything about his experience, skills or projects - for example \"What has he built with Go?\" or \"ประสบการณ์ทำงานของเขามีอะไรบ้าง\""

	resetDone = "Chat cleared - let's start over! Ask me anything about Chokchai."

	unavailableText = "Sorry, the assistant is unavailable right now. Please try again in a moment.\nขออภัย ตอนนี้ผู้ช่วยไม่พร้อมใช้งาน กรุณาลองใหม่อีกครั้ง"

	// offTopicText covers classifier verdicts this channel doesn't serve
	// (reminder/image intents inherited from the shared classifier).
	offTopicText = "I'm just a Q&A assistant for Chokchai's portfolio - I can't set reminders or draw pictures. Ask me about his experience, skills or projects instead!"
)

// isResetCommand recognizes the widget's "Clear chat" message, matching the
// LINE channel's spellings.
func isResetCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/reset", "ล้าง", "เริ่มใหม่":
		return true
	default:
		return false
	}
}

// respond computes the answer for one raw request payload.
func (c *Consumer) respond(ctx context.Context, data []byte) Response {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		log.Error().Err(err).Msg("webchat: failed to unmarshal request")
		return Response{Error: "invalid request"}
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return Response{Error: "missing session_id"}
	}
	query := strings.TrimSpace(req.Message)
	if query == "" {
		return Response{Text: usageHint}
	}
	if len([]rune(query)) > maxMessageChars {
		return Response{Error: "message too long"}
	}

	userID := userIDPrefix + strings.TrimSpace(req.SessionID)
	log.Info().Str("user_id", userID).Int("chars", len(query)).Msg("webchat: request received")

	if isResetCommand(query) {
		if err := c.store.Clear(ctx, userID); err != nil {
			log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to clear history")
			return Response{Text: "Sorry, I couldn't reset the conversation. Please try again."}
		}
		return Response{Text: resetDone}
	}

	history, err := c.store.GetRecent(ctx, userID)
	if err != nil {
		// Degrade to a context-less answer rather than failing the request.
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to load history - continuing without context")
		history = nil
	}

	start := time.Now()
	result, err := c.ai.Route(ctx, history, c.augment(ctx, query), nil)
	if err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: llm request failed")
		return Response{Text: unavailableText}
	}
	if result.ReminderIntent || result.ImageData != nil {
		return Response{Text: offTopicText}
	}
	log.Info().Str("user_id", userID).Dur("duration", time.Since(start)).Int("answer_chars", len(result.Text)).Msg("webchat: answered")

	if err := c.store.Append(ctx, userID, store.RoleUser, query); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to store user message")
	}
	if err := c.store.Append(ctx, userID, store.RoleModel, result.Text); err != nil {
		log.Error().Str("user_id", userID).Err(err).Msg("webchat: failed to store model reply")
	}
	return Response{Text: result.Text}
}
