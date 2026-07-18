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

	"consumer-llm-processor/internal/ai"
	"consumer-llm-processor/internal/consumer"
	"consumer-llm-processor/internal/store"
)

const (
	historyLimit = 20
	cacheTTL     = 30 * time.Minute
)

type Config struct {
	NatsURL      string
	NatsUser     string
	NatsPassword string
	GeminiAPIKey string
	GeminiModel  string
	// Optional free-tier providers; each is enabled by setting its API key.
	GroqAPIKey          string
	GroqModel           string
	GroqClassifierModel string
	OpenRouterAPIKey    string
	OpenRouterModel     string
	DatabaseURL         string
	RedisAddr           string
	RedisUsername       string
	RedisPassword       string
}

func loadConfig() *Config {
	return &Config{
		NatsURL:             getEnv("NATS_URL", nats.DefaultURL),
		NatsUser:            getEnv("NATS_USER", ""),
		NatsPassword:        getEnv("NATS_PASSWORD", ""),
		GeminiAPIKey:        getEnv("GEMINI_API_KEY", ""),
		GeminiModel:         getEnv("GEMINI_MODEL", "gemini-3.1-flash-lite"),
		GroqAPIKey:          getEnv("GROQ_API_KEY", ""),
		GroqModel:           getEnv("GROQ_MODEL", "llama-3.3-70b-versatile"),
		GroqClassifierModel: getEnv("GROQ_CLASSIFIER_MODEL", "llama-3.1-8b-instant"),
		OpenRouterAPIKey:    getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:     getEnv("OPENROUTER_MODEL", "deepseek/deepseek-r1:free"),
		DatabaseURL:         getEnv("DATABASE_URL", ""),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisUsername:       getEnv("REDIS_USERNAME", ""),
		RedisPassword:       getEnv("REDIS_PASSWORD", ""),
	}
}

// buildRouter assembles the difficulty router from whichever providers have
// API keys configured. Gemini alone still works: every tier then falls back
// to it.
func buildRouter(gemini *ai.Gemini, config *Config) *ai.Router {
	var groq, openrouter ai.Provider
	if config.GroqAPIKey != "" {
		groq = ai.NewOpenAI("groq", "https://api.groq.com/openai/v1", config.GroqAPIKey, config.GroqModel, ai.PersonaInstruction)
		log.Info().Str("model", config.GroqModel).Msg("startup: groq provider enabled")
	}
	if config.OpenRouterAPIKey != "" {
		openrouter = ai.NewOpenAI("openrouter", "https://openrouter.ai/api/v1", config.OpenRouterAPIKey, config.OpenRouterModel, ai.PersonaInstruction)
		log.Info().Str("model", config.OpenRouterModel).Msg("startup: openrouter provider enabled")
	}

	// Classifier: a tiny Groq model when available (fast, generous free
	// quota), otherwise Gemini classifies its own traffic.
	var classifier ai.Provider
	if config.GroqAPIKey != "" {
		classifier = ai.NewOpenAI("groq-classifier", "https://api.groq.com/openai/v1", config.GroqAPIKey, config.GroqClassifierModel, ai.ClassifierInstruction)
	} else {
		classifier = gemini.Derive("gemini-classifier", ai.ClassifierInstruction, false)
	}

	geminiDeep := gemini.Derive("gemini/"+config.GeminiModel+"+think", ai.PersonaInstruction, true)

	chain := func(providers ...ai.Provider) []ai.Provider {
		out := make([]ai.Provider, 0, len(providers))
		for _, p := range providers {
			if p != nil {
				out = append(out, p)
			}
		}
		return out
	}
	// Priorities assume GEMINI_MODEL is a lite/high-quota model
	// (gemini-3.1-flash-lite): it leads small talk, Groq's 70B leads real
	// questions, and reasoning models lead technical with Gemini deep
	// thinking (then plain) as last resorts.
	return ai.NewRouter(classifier,
		chain(gemini, groq, openrouter),             // simple
		chain(groq, gemini, openrouter),             // general
		chain(openrouter, groq, geminiDeep, gemini), // technical
	)
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
	if config.GeminiAPIKey == "" || config.DatabaseURL == "" {
		log.Fatal().Msg("startup: GEMINI_API_KEY and DATABASE_URL must be set")
	}

	ctx := context.Background()

	// Conversation store: shared Redis cache first, Postgres fallback.
	pg, err := store.NewPostgres(ctx, config.DatabaseURL, historyLimit)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to init postgres store")
	}
	log.Info().Msg("startup: postgres store ready")

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Username: config.RedisUsername,
		Password: config.RedisPassword,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		// Cache is best-effort: keep running on Postgres alone.
		log.Error().Str("addr", config.RedisAddr).Err(err).Msg("startup: redis unreachable - degrading to postgres only")
	} else {
		log.Info().Str("addr", config.RedisAddr).Msg("startup: redis cache connected")
	}
	conversations := store.NewCached(pg, rdb, historyLimit, cacheTTL)
	defer conversations.Close()

	gemini, err := ai.New(ctx, config.GeminiAPIKey, config.GeminiModel)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to init gemini client")
	}
	log.Info().Str("model", config.GeminiModel).Msg("startup: gemini client ready")

	router := buildRouter(gemini, config)

	nc, err := nats.Connect(config.NatsURL,
		nats.UserInfo(config.NatsUser, config.NatsPassword),
		nats.Name("consumer-llm-processor"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		log.Fatal().Str("url", config.NatsURL).Err(err).Msg("startup: failed to connect to NATS")
	}
	defer nc.Drain()
	log.Info().Str("url", config.NatsURL).Msg("startup: connected to NATS")

	c := consumer.New(conversations, router, nc)
	sub, err := c.Subscribe()
	if err != nil {
		log.Fatal().Str("subject", consumer.RequestSubject).Err(err).Msg("startup: failed to subscribe")
	}
	defer sub.Unsubscribe()
	log.Info().
		Str("subject", consumer.RequestSubject).
		Str("queue", consumer.QueueGroup).
		Str("reply_subject", consumer.ReplySubject).
		Msg("startup: subscribed - consumer running")

	// Pure consumer: no HTTP server, just block until asked to shut down.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")
}
