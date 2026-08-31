// portfolio-chat-gateway is the HTTP entry point for the portfolio
// website's chat widget: it validates and rate-limits visitor messages,
// relays them to consumer-llm-processor over NATS request-reply
// (portfolio.chat.ai_request) and returns the answer. It is deployed as a
// ClusterIP-only service - the website's Next.js /api/chat route handler is
// its sole caller, so it never faces the public internet directly.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	"github.com/Chokchai-Fa/homelab-monorepo/libs/natsutil"

	"portfolio-chat-gateway/internal/handler"
)

type Config struct {
	Port         string
	NatsURL      string
	NatsUser     string
	NatsPassword string
	// RequestTimeout bounds one NATS request-reply round trip; keep it
	// above consumer-llm-processor's generate timeout (55s) so the consumer
	// answers first.
	RequestTimeout time.Duration
	// RateLimitPerMin is the per-visitor-IP message budget; the gateway
	// fronts free-tier LLM quotas, so keep this modest.
	RateLimitPerMin int
	MaxMessageChars int
}

func loadConfig() *Config {
	return &Config{
		Port:            getEnv("PORT", "8081"),
		NatsURL:         getEnv("NATS_URL", ""),
		NatsUser:        getEnv("NATS_USER", ""),
		NatsPassword:    getEnv("NATS_PASSWORD", ""),
		RequestTimeout:  getEnvDuration("REQUEST_TIMEOUT", 60*time.Second),
		RateLimitPerMin: getEnvInt("RATE_LIMIT_PER_MIN", 10),
		MaxMessageChars: getEnvInt("MAX_MESSAGE_CHARS", 1000),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Error().Str("key", key).Str("value", v).Msg("config: invalid integer - using default")
		return defaultValue
	}
	return n
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Error().Str("key", key).Str("value", v).Err(err).Msg("config: invalid duration - using default")
		return defaultValue
	}
	return d
}

// visitorIP identifies a visitor for rate limiting. Traffic arrives through
// cloudflared -> portfolio-web's proxy, so the socket peer is always a pod
// IP; the real visitor is in CF-Connecting-IP (forwarded by the proxy) or
// X-Forwarded-For.
func visitorIP(c echo.Context) (string, error) {
	if ip := c.Request().Header.Get("CF-Connecting-IP"); ip != "" {
		return ip, nil
	}
	return c.RealIP(), nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Info().Err(err).Msg("startup: .env not loaded")
	}

	level, err := zerolog.ParseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	config := loadConfig()

	// NATS is the gateway's only backend. RetryOnFailedConnect keeps the
	// process alive through NATS restarts; requests answer 503 meanwhile.
	// natsClosing tells the ClosedHandler that this shutdown is intentional
	// (a Drain on SIGTERM also closes the connection), so it doesn't mistake a
	// graceful stop for a lost connection and exit non-zero.
	var nc *nats.Conn
	var natsClosing atomic.Bool
	if config.NatsURL != "" {
		nc, err = nats.Connect(config.NatsURL,
			nats.UserInfo(config.NatsUser, config.NatsPassword),
			nats.Name("portfolio-chat-gateway"),
			nats.RetryOnFailedConnect(true),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(2*time.Second),
			nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
				log.Warn().Err(err).Msg("nats: disconnected - will retry")
			}),
			nats.ReconnectHandler(func(nc *nats.Conn) {
				log.Info().Str("url", nc.ConnectedUrl()).Msg("nats: reconnected")
			}),
			// MaxReconnects(-1) retries forever, but a connection can still reach
			// the terminal CLOSED state. NATS is this gateway's only backend, so a
			// dead connection means every request fails until restarted by hand;
			// exit and let Kubernetes restart it with a fresh connection instead.
			nats.ClosedHandler(func(_ *nats.Conn) {
				if natsClosing.Load() {
					return
				}
				log.Error().Msg("nats: connection closed permanently - exiting for restart")
				os.Exit(1)
			}),
		)
		if err != nil {
			log.Error().Str("url", config.NatsURL).Err(err).Msg("startup: NATS unavailable - chat requests will fail")
			nc = nil
		} else {
			defer nc.Drain()
			natsutil.StartWatchdog(nc, &natsClosing)
			log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")
		}
	} else {
		log.Error().Msg("startup: NATS_URL not set - chat requests will fail")
	}

	var backend handler.Backend
	if nc != nil {
		backend = &natsBackend{conn: nc}
	}
	h := handler.New(backend, config.RequestTimeout, config.MaxMessageChars)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())

	// One rate limiter shared by both chat entry points so a visitor can't
	// dodge the budget by mixing streaming and non-streaming calls.
	rateLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
			Rate:      rate.Limit(float64(config.RateLimitPerMin) / 60.0),
			Burst:     config.RateLimitPerMin,
			ExpiresIn: 10 * time.Minute,
		}),
		IdentifierExtractor: visitorIP,
		ErrorHandler: func(c echo.Context, _ error) error {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "could not identify caller"})
		},
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "slow down a little - try again in a minute"})
		},
	})

	e.GET("/healthz", h.Healthz)
	e.GET("/status", h.Status)
	e.POST("/chat", h.Chat, rateLimiter)
	e.POST("/chat/stream", h.ChatStream, rateLimiter)

	// Serve until signalled, then drain in-flight requests.
	go func() {
		if err := e.Start(":" + config.Port); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("startup: server failed")
		}
	}()
	log.Info().Str("port", config.Port).Msg("startup: server started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	natsClosing.Store(true)
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("shutdown: server shutdown failed")
	}
}

// natsBackend adapts *nats.Conn to handler.Backend: a unary request-reply and
// a streaming open (fresh inbox + sync subscription the SSE handler drains).
type natsBackend struct {
	conn *nats.Conn
}

func (b *natsBackend) Request(subj string, data []byte, timeout time.Duration) (*nats.Msg, error) {
	return b.conn.Request(subj, data, timeout)
}

func (b *natsBackend) Connected() bool { return b.conn.IsConnected() }

func (b *natsBackend) OpenStream(subject string, payload []byte) (handler.StreamSub, error) {
	inbox := b.conn.NewInbox()
	sub, err := b.conn.SubscribeSync(inbox)
	if err != nil {
		return nil, err
	}
	if err := b.conn.PublishRequest(subject, inbox, payload); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	return &natsStreamSub{sub: sub}, nil
}

type natsStreamSub struct {
	sub *nats.Subscription
}

func (s *natsStreamSub) Next(timeout time.Duration) ([]byte, error) {
	msg, err := s.sub.NextMsg(timeout)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

func (s *natsStreamSub) Close() error { return s.sub.Unsubscribe() }
