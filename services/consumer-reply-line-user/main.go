package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/v7/linebot"
	"github.com/nats-io/nats.go"

	"consumer-reply-line-user/internal/consumer"
)

type Config struct {
	NatsURL       string
	NatsUser      string
	NatsPassword  string
	ChannelSecret string
	ChannelToken  string
}

func loadConfig() *Config {
	return &Config{
		NatsURL:       getEnv("NATS_URL", nats.DefaultURL),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		ChannelSecret: getEnv("LINE_CHANNEL_SECRET", ""),
		ChannelToken:  getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
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
	if config.ChannelSecret == "" || config.ChannelToken == "" {
		log.Fatal("LINE_CHANNEL_SECRET and LINE_CHANNEL_ACCESS_TOKEN must be set")
	}

	bot, err := linebot.New(config.ChannelSecret, config.ChannelToken)
	if err != nil {
		log.Fatalf("Failed to init LINE client: %v", err)
	}

	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-reply-line-user"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to connect to NATS at %s: %v", config.NatsURL, err)
	}
	defer nc.Drain()

	c := consumer.New(bot)
	sub, err := c.Subscribe(nc)
	if err != nil {
		log.Fatalf("Failed to subscribe to %s: %v", consumer.Subject, err)
	}
	defer sub.Unsubscribe()
	log.Printf("Subscribed to %s (queue %s)", consumer.Subject, consumer.QueueGroup)

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Printf("Received %s, shutting down", s)
}
