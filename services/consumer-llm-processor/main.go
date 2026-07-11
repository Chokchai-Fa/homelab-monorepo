package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"consumer-llm-processor/internal/ai"
	"consumer-llm-processor/internal/consumer"
	"consumer-llm-processor/internal/store"
)

const (
	historyLimit = 20
	cacheTTL     = 30 * time.Minute
)

type Config struct {
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	GeminiAPIKey  string
	GeminiModel   string
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
		GeminiAPIKey:  getEnv("GEMINI_API_KEY", ""),
		GeminiModel:   getEnv("GEMINI_MODEL", "gemini-3.5-flash"),
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
		log.Printf(".env not found or failed to load: %v", err)
	}

	config := loadConfig()
	if config.GeminiAPIKey == "" || config.DatabaseURL == "" {
		log.Fatal("GEMINI_API_KEY and DATABASE_URL must be set")
	}

	ctx := context.Background()

	// Conversation store: shared Redis cache first, Postgres fallback.
	pg, err := store.NewPostgres(ctx, config.DatabaseURL, historyLimit)
	if err != nil {
		log.Fatalf("Failed to init postgres store: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Username: config.RedisUsername,
		Password: config.RedisPassword,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		// Cache is best-effort: keep running on Postgres alone.
		log.Printf("Redis unreachable at %s (degrading to postgres only): %v", config.RedisAddr, err)
	}
	conversations := store.NewCached(pg, rdb, historyLimit, cacheTTL)
	defer conversations.Close()

	gemini, err := ai.New(ctx, config.GeminiAPIKey, config.GeminiModel)
	if err != nil {
		log.Fatalf("Failed to init gemini client: %v", err)
	}

	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-llm-processor"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to connect to NATS at %s: %v", config.NatsURL, err)
	}
	defer nc.Drain()

	c := consumer.New(conversations, gemini, nc)
	sub, err := c.Subscribe()
	if err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", consumer.RequestSubject, err)
	}
	defer sub.Unsubscribe()
	log.Printf("Subscribed to %s (queue %s), model %s, replies to %s",
		consumer.RequestSubject, consumer.QueueGroup, config.GeminiModel, consumer.ReplySubject)

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("Received %s, shutting down", s)
}
