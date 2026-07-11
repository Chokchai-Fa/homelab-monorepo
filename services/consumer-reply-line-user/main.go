package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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
	Port          string
}

func loadConfig() *Config {
	return &Config{
		NatsURL:       getEnv("NATS_URL", nats.DefaultURL),
		NatsUser:      getEnv("NATS_USER", ""),
		NatsPassword:  getEnv("NATS_PASSWORD", ""),
		ChannelSecret: getEnv("LINE_CHANNEL_SECRET", ""),
		ChannelToken:  getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
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

	// Health endpoint for k8s probes.
	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.GET("/health", func(c echo.Context) error {
		if !nc.IsConnected() {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{
				"status": "degraded", "message": "NATS disconnected",
			})
		}
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ok", "message": "LINE reply consumer is running",
		})
	})

	log.Printf("Starting server on port %s", config.Port)
	e.Logger.Fatal(e.Start(":" + config.Port))
}
