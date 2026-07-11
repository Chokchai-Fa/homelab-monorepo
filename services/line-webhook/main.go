package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/line/line-bot-sdk-go/v7/linebot"

	"line-webhook/internal/handler"
	"line-webhook/internal/router"
)

type Config struct {
	ChannelSecret string
	ChannelToken  string
	Port          string
}

func loadConfig() *Config {
	return &Config{
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

	// Initialize LINE Bot client
	bot, err := linebot.New(config.ChannelSecret, config.ChannelToken)
	if err != nil {
		log.Fatal(err)
	}

	// Initialize Echo via router package and start server
	e := router.NewRouter(router.RouterOptions{
		Echo:   nil, // router will create a new Echo instance if nil
		Config: &handler.Config{ChannelSecret: config.ChannelSecret},
		Bot:    bot,
	})

	log.Printf("Starting server on port %s", config.Port)
	e.Logger.Fatal(e.Start(":" + config.Port))
}
