package main

import (
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"

	"consumer-reply-line-user/internal/consumer"
)

type Config struct {
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	ChannelSecret string
	ChannelToken  string
	// ImageBaseURL is line-webhook's public base URL; generated images are
	// sent to LINE as <ImageBaseURL>/images/<key>. Empty disables image
	// replies (their text still goes out).
	ImageBaseURL string
}

func loadConfig() *Config {
	return &Config{
		NatsURL:       getEnv("NATS_URL", nats.DefaultURL),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		ChannelSecret: getEnv("LINE_CHANNEL_SECRET", ""),
		ChannelToken:  getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		ImageBaseURL:  getEnv("IMAGE_BASE_URL", ""),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Info().Err(err).Msg("startup: .env not loaded")
	}

	config := loadConfig()
	if config.ChannelSecret == "" || config.ChannelToken == "" {
		log.Fatal().Msg("startup: LINE_CHANNEL_SECRET and LINE_CHANNEL_ACCESS_TOKEN must be set")
	}

	bot, err := linebot.New(config.ChannelSecret, config.ChannelToken)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to init LINE client")
	}
	log.Info().Msg("startup: LINE client ready")

	// natsClosing tells the ClosedHandler that this shutdown is intentional
	// (a Drain on SIGTERM also closes the connection), so it doesn't mistake a
	// graceful stop for a lost connection and exit non-zero.
	var natsClosing atomic.Bool
	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-reply-line-user"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn().Err(err).Msg("nats: disconnected - will retry")
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Info().Str("url", nc.ConnectedUrl()).Msg("nats: reconnected")
		}),
		// MaxReconnects(-1) retries forever, but a connection can still reach the
		// terminal CLOSED state (e.g. a permanent auth/handshake failure). This
		// consumer would then sit alive with dead subscriptions until restarted
		// by hand, so exit and let Kubernetes restart it with a fresh connection.
		nats.ClosedHandler(func(_ *nats.Conn) {
			if natsClosing.Load() {
				return
			}
			log.Error().Msg("nats: connection closed permanently - exiting for restart")
			os.Exit(1)
		}),
	)
	if err != nil {
		log.Fatal().Str("url", config.NatsURL).Err(err).Msg("startup: failed to connect to NATS")
	}
	defer nc.Drain()
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	if config.ImageBaseURL == "" {
		log.Info().Msg("startup: IMAGE_BASE_URL not set - generated-image replies disabled")
	}
	c := consumer.New(bot, config.ImageBaseURL)
	sub, err := c.Subscribe(nc)
	if err != nil {
		log.Fatal().Str("subject", consumer.Subject).Err(err).Msg("startup: failed to subscribe")
	}
	defer sub.Unsubscribe()
	log.Info().Str("subject", consumer.Subject).Str("queue", consumer.QueueGroup).Msg("startup: subscribed - consumer running")

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	natsClosing.Store(true)
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")
}
