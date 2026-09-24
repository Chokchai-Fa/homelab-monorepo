package main

import (
	"context"
	"encoding/json"
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

	"consumer-reminder/internal/consumer"
	"consumer-reminder/internal/events"
	"consumer-reminder/internal/flow"
	"consumer-reminder/internal/store"
)

type Config struct {
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	DatabaseURL   string
	RedisAddr     string
	RedisUsername string
	RedisPassword string
	FlowTTL       time.Duration
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
		FlowTTL:       getEnvDuration("FLOW_TTL", 10*time.Minute),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
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

func main() {
	if err := godotenv.Load(); err != nil {
		log.Info().Err(err).Msg("startup: .env not loaded")
	}

	config := loadConfig()
	if config.DatabaseURL == "" {
		log.Fatal().Msg("startup: DATABASE_URL must be set")
	}

	ctx := context.Background()

	db, err := store.New(ctx, config.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: postgres init failed")
	}
	defer db.Close()
	log.Info().Msg("startup: postgres ready (line_users, reminders)")

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Username: config.RedisUsername,
		Password: config.RedisPassword,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		// Flow state cannot live anywhere else, so this one is fatal.
		log.Fatal().Str("addr", config.RedisAddr).Err(err).Msg("startup: redis unreachable")
	}
	log.Info().Str("addr", config.RedisAddr).Dur("flow_ttl", config.FlowTTL).Msg("startup: redis ready")

	// natsClosing tells the ClosedHandler that this shutdown is intentional
	// (a Drain on SIGTERM also closes the connection), so it doesn't mistake a
	// graceful stop for a lost connection and exit non-zero.
	var natsClosing atomic.Bool
	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-reminder"),
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
	natsutil.StartWatchdog(nc, &natsClosing)
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	// JetStream carries the durable LINE chat pipeline (memory-backed, so no SD
	// writes): reminder requests, postbacks and profile events in, replies out.
	js, err := nc.JetStream()
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to get JetStream context")
	}
	if err := natsutil.EnsureLineChatStream(js); err != nil {
		log.Fatal().Str("stream", natsutil.LineChatStream).Err(err).Msg("startup: failed to ensure JetStream stream")
	}
	log.Info().Str("stream", natsutil.LineChatStream).Msg("startup: JetStream stream ready")

	publish := func(ev events.ReplyEvent) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = js.Publish(events.ReplySubject, data)
		return err
	}

	states := flow.NewStateStore(rdb, config.FlowTTL)
	fl := flow.New(db, states, publish)

	c := consumer.New(fl, db)
	subs, err := c.Subscribe(js)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to subscribe")
	}
	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()

	// Subscribe can report success locally while a server-side durable
	// consumer silently fails to persist (see natsutil.VerifyConsumer) -
	// confirm each of the three durables this service owns actually exists
	// before declaring the pod healthy, and keep checking: exiting hands a
	// broken subscription to Kubernetes as a restart instead of leaving the
	// pod alive and quietly dropping reminder requests, postbacks or
	// profile updates.
	for _, durable := range []string{
		consumer.QueueGroup + "-request",
		consumer.QueueGroup + "-postback",
		consumer.QueueGroup + "-profile",
	} {
		if err := natsutil.VerifyConsumer(js, durable); err != nil {
			log.Fatal().Str("durable", durable).Err(err).Msg("startup: subscribed but durable consumer missing on server - exiting")
		}
		natsutil.StartConsumerWatchdog(js, durable, &natsClosing)
	}
	log.Info().Str("queue", consumer.QueueGroup).Msg("startup: subscribed - consumer running")

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	natsClosing.Store(true)
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")
}
