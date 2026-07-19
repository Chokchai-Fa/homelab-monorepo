package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

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

	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("subscriber-reminder-notifier"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Fatal().Str("url", config.NatsURL).Err(err).Msg("startup: failed to connect to NATS")
	}
	defer nc.Drain()
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	n := notifier.New(db, rdb, nc)

	sub, err := n.SubscribeDelivery(nc, queueGroup)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to subscribe to delivery acks")
	}
	defer sub.Unsubscribe()
	log.Info().Msg("startup: subscribed to delivery acks - notifier running")

	if err := n.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error().Err(err).Msg("notifier: Run exited unexpectedly")
	}
	log.Info().Msg("shutdown: signal received")
}
