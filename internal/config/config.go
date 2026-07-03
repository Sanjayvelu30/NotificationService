package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	HTTPAddr    string
	WorkerCount int
}

func Load() (Config, error) {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL environment variable is required and cannot be empty")
	}

	return Config{
		DatabaseURL: dbURL,
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		WorkerCount: getEnvInt("WORKER_COUNT", 5),
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
