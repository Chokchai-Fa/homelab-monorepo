package main

import (
	"context"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"line-webhook/internal/handler"
	"line-webhook/internal/publisher"
	"line-webhook/internal/router"
	"line-webhook/internal/session"
)

type Config struct {
	ChannelSecret string
	ChannelToken  string
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	AIPrefix      string
	RedisAddr     string
	RedisUsername string
	RedisPassword string
	SessionTTL    time.Duration
	Port          string
}

func loadConfig() *Config {
	ttl := 10 * time.Minute
	if v := os.Getenv("AI_SESSION_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		} else {
			log.Error().Str("value", v).Err(err).Msg("config: invalid AI_SESSION_TTL - using default 10m")
		}
	}
	return &Config{
		ChannelSecret: getEnv("LINE_CHANNEL_SECRET", ""),
		ChannelToken:  getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		NatsURL:       getEnv("NATS_URL", ""),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		AIPrefix:      getEnv("AI_PREFIX", "/ai"),
		RedisAddr:     getEnv("REDIS_ADDR", ""),
		RedisUsername: getEnv("REDIS_USERNAME", ""),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		SessionTTL:    ttl,
		Port:          getEnv("PORT", "8080"),
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

	if config.ChannelSecret == "" {
		log.Fatal().Msg("startup: LINE_CHANNEL_SECRET must be set")
	}

	// NATS publisher: the webhook never replies to LINE itself, it only
	// produces events (AI requests and replies) for the downstream consumers.
	// Non-fatal on failure so the webhook keeps accepting LINE events.
	var pub handler.EventPublisher
	if config.NatsURL != "" {
		p, err := publisher.New(config.NatsURL, config.NatsUser, config.NatsPassword)
		if err != nil {
			log.Error().Str("url", config.NatsURL).Err(err).Msg("startup: NATS unavailable - incoming messages will be dropped")
		} else {
			defer p.Close()
			pub = p
			log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")
		}
	} else {
		log.Error().Msg("startup: NATS_URL not set - incoming messages will be dropped")
	}

	// AI session store in Redis (sliding TTL). Non-fatal on failure: without
	// it only the explicit prefix routes messages to the AI.
	var sessions handler.SessionStore
	if config.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{
			Addr:     config.RedisAddr,
			Username: config.RedisUsername,
			Password: config.RedisPassword,
		})
		if err := rdb.Ping(context.Background()).Err(); err != nil {
			log.Error().Str("addr", config.RedisAddr).Err(err).Msg("startup: redis unreachable - AI session mode degraded to prefix-only")
		} else {
			log.Info().Str("addr", config.RedisAddr).Dur("ttl", config.SessionTTL).Msg("startup: AI session store ready")
		}
		sessions = session.New(rdb, config.SessionTTL)
	} else {
		log.Info().Msg("startup: REDIS_ADDR not set - AI session mode disabled (prefix-only)")
	}

	var bot *linebot.Client
	if config.ChannelSecret != "" && config.ChannelToken != "" {
		var err error
		bot, err = linebot.New(config.ChannelSecret, config.ChannelToken)
		if err != nil {
			log.Error().Err(err).Msg("startup: failed to initialize LINE bot client")
		} else {
			log.Info().Msg("startup: LINE bot client ready")
		}
	}

	// Initialize Echo via router package and start server
	e := router.NewRouter(router.RouterOptions{
		Echo:      nil, // router will create a new Echo instance if nil
		Config:    &handler.Config{ChannelSecret: config.ChannelSecret, ChannelToken: config.ChannelToken, AIPrefix: config.AIPrefix},
		Publisher: pub,
		Sessions:  sessions,
		Bot:       bot,
	})

	log.Info().Str("port", config.Port).Msg("startup: server starting")
	e.Logger.Fatal(e.Start(":" + config.Port))
}
