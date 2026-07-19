package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"worker-reminder-scheduler/internal/scheduler"
	"worker-reminder-scheduler/internal/store"
)

type Config struct {
	DatabaseURL   string
	RedisAddr     string
	RedisUsername string
	RedisPassword string
	// ArmHorizon: reminders due within this window get armed as an expiring
	// Redis key this tick.
	ArmHorizon time.Duration
	// Tick is how often the scheduling pass runs.
	Tick time.Duration
}

func loadConfig() *Config {
	return &Config{
		DatabaseURL:   getEnv("DATABASE_URL", ""),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisUsername: getEnv("REDIS_USERNAME", ""),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		ArmHorizon:    getEnvDuration("ARM_HORIZON", 5*time.Minute),
		Tick:          getEnvDuration("TICK", time.Minute),
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
	log.Info().Msg("startup: postgres ready (reminders)")

	rdb := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Username: config.RedisUsername,
		Password: config.RedisPassword,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal().Str("addr", config.RedisAddr).Err(err).Msg("startup: redis unreachable")
	}
	log.Info().Str("addr", config.RedisAddr).Msg("startup: redis ready")

	sched := scheduler.New(db, rdb, config.ArmHorizon)

	cron, err := gocron.NewScheduler()
	if err != nil {
		log.Fatal().Err(err).Msg("startup: gocron init failed")
	}
	_, err = cron.NewJob(
		gocron.DurationJob(config.Tick),
		gocron.NewTask(func() {
			passCtx, cancel := context.WithTimeout(context.Background(), config.Tick)
			defer cancel()
			sched.RunPass(passCtx)
		}),
		gocron.WithStartAt(gocron.WithStartImmediately()),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("startup: failed to schedule job")
	}
	cron.Start()
	defer func() {
		if err := cron.Shutdown(); err != nil {
			log.Error().Err(err).Msg("shutdown: gocron shutdown failed")
		}
	}()
	log.Info().Dur("tick", config.Tick).Dur("arm_horizon", config.ArmHorizon).Msg("startup: scheduler running")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	s := <-sig
	log.Info().Str("signal", s.String()).Msg("shutdown: signal received")
}
