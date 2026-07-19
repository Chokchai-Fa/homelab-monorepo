package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

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

	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-reminder"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Fatal().Str("url", config.NatsURL).Err(err).Msg("startup: failed to connect to NATS")
	}
	defer nc.Drain()
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	publish := func(ev events.ReplyEvent) error {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		return nc.Publish(events.ReplySubject, data)
	}

	states := flow.NewStateStore(rdb, config.FlowTTL)
	fl := flow.New(db, states, publish)

	c := consumer.New(fl, db)
	subs, err := c.Subscribe(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to subscribe")
	}
	defer func() {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}()
	log.Info().Str("queue", consumer.QueueGroup).Msg("startup: subscribed - consumer running")

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")
}
