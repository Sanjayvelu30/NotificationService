package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost          string
	DBPort          string
	DBUser          string
	DBPassword      string
	DBName          string
	DBSSLMode       string
	RedisHost       string
	RedisPort       string
	RedisPassword   string
	WorkerCount     int
	QueueCapacity   int
	SchedulerAPIKey string
}

func Load() (Config, error) {
	// Load environment variables from .env file if it exists
	_ = godotenv.Load()

	// Parse database credentials
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgres_password")
	dbName := getEnv("DB_NAME", "tasks")
	dbSSLMode := getEnv("DB_SSLMODE", "disable")

	// Validate DB configuration
	if dbHost == "" || dbUser == "" || dbName == "" {
		return Config{}, fmt.Errorf("missing critical database configuration (DB_HOST, DB_USER, DB_NAME)")
	}

	workerCount := getEnvInt("WORKER_COUNT", 5)
	queueCapacity := getEnvInt("QUEUE_CAPACITY", 10)

	return Config{
		DBHost:          dbHost,
		DBPort:          dbPort,
		DBUser:          dbUser,
		DBPassword:      dbPassword,
		DBName:          dbName,
		DBSSLMode:       dbSSLMode,
		RedisHost:       getEnv("REDIS_HOST", "localhost"),
		RedisPort:       getEnv("REDIS_PORT", "6379"),
		RedisPassword:   getEnv("REDIS_PASSWORD", ""),
		WorkerCount:     workerCount,
		QueueCapacity:   queueCapacity,
		SchedulerAPIKey: getEnv("SCHEDULER_API_KEY", ""),
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
