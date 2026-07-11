package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"

	"line-webhook/internal/handler"
	"line-webhook/internal/publisher"
	"line-webhook/internal/router"
)

type Config struct {
	ChannelSecret string
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	AIPrefix      string
	Port          string
}

func loadConfig() *Config {
	return &Config{
		ChannelSecret: getEnv("LINE_CHANNEL_SECRET", ""),
		NatsURL:       getEnv("NATS_URL", ""),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		AIPrefix:      getEnv("AI_PREFIX", "/ai"),
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
		log.Printf(".env not found or failed to load: %v", err)
	}

	config := loadConfig()

	if config.ChannelSecret == "" {
		log.Fatal("LINE_CHANNEL_SECRET must be set")
	}

	// NATS publisher: the webhook never replies to LINE itself, it only
	// produces events (AI requests and replies) for the downstream consumers.
	// Non-fatal on failure so the webhook keeps accepting LINE events.
	var pub handler.EventPublisher
	if config.NatsURL != "" {
		p, err := publisher.New(config.NatsURL, config.NatsUser, config.NatsPassword)
		if err != nil {
			log.Printf("NATS unavailable at %s (incoming messages will be dropped): %v", config.NatsURL, err)
		} else {
			defer p.Close()
			pub = p
			log.Printf("Connected to NATS at %s", config.NatsURL)
		}
	} else {
		log.Printf("NATS_URL not set; incoming messages will be dropped")
	}

	// Initialize Echo via router package and start server
	e := router.NewRouter(router.RouterOptions{
		Echo:      nil, // router will create a new Echo instance if nil
		Config:    &handler.Config{ChannelSecret: config.ChannelSecret, AIPrefix: config.AIPrefix},
		Publisher: pub,
	})

	log.Printf("Starting server on port %s", config.Port)
	e.Logger.Fatal(e.Start(":" + config.Port))
}
