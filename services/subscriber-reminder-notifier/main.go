package main

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/Chokchai-Fa/homelab-monorepo/libs/natsutil"

	"subscriber-reminder-notifier/internal/notifier"
	"subscriber-reminder-notifier/internal/store"
)

const queueGroup = "subscriber-reminder-notifier"

type Config struct {
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	DatabaseURL   string
	RedisAddr     string
	RedisUsername string
	RedisPassword string
}

func loadConfig() *Config {
	return &Config{
		NatsURL:       getEnv("NATS_URL", nats.DefaultURL),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisUsername: getEnv("REDIS_USERNAME", ""),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
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
	if config.DatabaseURL == "" {
		log.Fatal().Msg("startup: DATABASE_URL must be set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.New(ctx, config.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: postgres init failed")
	}
	defer db.Close()
	log.Info().Msg("startup: postgres ready (reminders, line_users)")

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Username: config.RedisUsername,
		Password: config.RedisPassword,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Str("addr", config.RedisAddr).Err(err).Msg("startup: redis unreachable")
	}
	log.Info().Str("addr", config.RedisAddr).Msg("startup: redis ready")

	// natsClosing tells the ClosedHandler that this shutdown is intentional
	// (a Drain on SIGTERM also closes the connection), so it doesn't mistake a
	// graceful stop for a lost connection and exit non-zero.
	var natsClosing atomic.Bool
	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("subscriber-reminder-notifier"),
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
		// subscriber would then sit alive with a dead subscription until restarted
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
	natsutil.StartWatchdog(nc, &natsClosing)
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	// JetStream carries the durable LINE chat pipeline (memory-backed, so no SD
	// writes): fired-reminder replies out, delivery acks in.
	js, err := nc.JetStream()
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to get JetStream context")
	}
	if err := natsutil.EnsureLineChatStream(js); err != nil {
		log.Fatal().Str("stream", natsutil.LineChatStream).Err(err).Msg("startup: failed to ensure JetStream stream")
	}
	log.Info().Str("stream", natsutil.LineChatStream).Msg("startup: JetStream stream ready")

	n := notifier.New(db, rdb, js)

	sub, err := n.SubscribeDelivery(js, queueGroup)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to subscribe to delivery acks")
	}
	defer sub.Unsubscribe()

	// Subscribe can report success locally while the server-side durable
	// consumer silently fails to persist (see natsutil.VerifyConsumer) -
	// confirm it actually exists before declaring this pod healthy, and
	// keep checking: exiting hands a broken subscription to Kubernetes as a
	// restart instead of leaving the pod alive and quietly dropping every
	// delivery ack, which would leave fired reminders stuck "pending"
	// forever.
	if err := natsutil.VerifyConsumer(js, queueGroup); err != nil {
		log.Fatal().Str("durable", queueGroup).Err(err).Msg("startup: subscribed but durable consumer missing on server - exiting")
	}
	natsutil.StartConsumerWatchdog(js, queueGroup, &natsClosing)
	log.Info().Msg("startup: subscribed to delivery acks - notifier running")

	if err := n.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error().Err(err).Msg("notifier: Run exited unexpectedly")
	}
	natsClosing.Store(true)
	log.Info().Msg("shutdown: signal received")
}
