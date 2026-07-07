package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL       string
	HTTPAddr          string
	WorkerCount       int
	RateLimitMax      int
	RateLimitDuration time.Duration
	Auth0Domain       string
	EmailFrom         string
	SchedulerURL      string
	SchedulerAPIKey   string
}

func Load() (Config, error) {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL environment variable is required and cannot be empty")
	}

	rlMax := getEnvInt("RATE_LIMIT_MAX", 3)
	rlDurationStr := getEnv("RATE_LIMIT_DURATION", "24h")
	rlDuration, err := time.ParseDuration(rlDurationStr)
	if err != nil {
		rlDuration = 24 * time.Hour
	}

	return Config{
		DatabaseURL:       dbURL,
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		WorkerCount:       getEnvInt("WORKER_COUNT", 5),
		RateLimitMax:      rlMax,
		RateLimitDuration: rlDuration,
		Auth0Domain:       getEnv("AUTH0_DOMAIN", "dev-i6avz7x124upwug6.us.auth0.com"),
		EmailFrom:         getEnv("EMAIL_FROM", "no-reply@sanjayvelu.online"),
		SchedulerURL:      getEnv("SCHEDULER_URL", "http://localhost:8081"),
		SchedulerAPIKey:   getEnv("SCHEDULER_API_KEY", "test_api_key_12345"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
