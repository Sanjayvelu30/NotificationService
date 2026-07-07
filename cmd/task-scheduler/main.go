package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/sanjay/NotificationService/internal/scheduler/config"
	"github.com/sanjay/NotificationService/internal/scheduler/handler"
	"github.com/sanjay/NotificationService/internal/scheduler/repository"
	"github.com/sanjay/NotificationService/internal/scheduler/service"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Load configuration variables utilizing godotenv
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Connect to PostgreSQL
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to initialize database connection: %v", err)
	}
	defer db.Close()

	// Verify database connection
	if err := db.Ping(); err != nil {
		log.Printf("Warning: Database ping failed (verify Postgres is running): %v", err)
	} else {
		fmt.Println("Connected to PostgreSQL successfully.")
	}

	// 3. Connect to Redis conditionally (skips if config host is local or unreachable)
	var rdb *redis.Client
	if cfg.RedisHost != "" && cfg.RedisHost != "localhost" && cfg.RedisHost != "127.0.0.1" {
		redisAddr := fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort)
		rdb = redis.NewClient(&redis.Options{
			Addr:     redisAddr,
			Password: cfg.RedisPassword,
			DB:       0,
		})
		defer rdb.Close()

		// Verify Redis connection
		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Printf("Warning: Redis ping failed (running in DB-only mode): %v", err)
			rdb = nil
		} else {
			fmt.Println("Connected to Redis successfully.")
		}
	} else {
		log.Println("Redis host is local or empty; running in Database-only mode without cache.")
	}

	// 4. Recreate/Initialize tasks schema
	// Drop tasks to clean up database column definitions
	dropQuery := `DROP TABLE IF EXISTS tasks;`
	if _, err := db.Exec(dropQuery); err != nil {
		log.Printf("Warning: Failed to drop tasks table: %v", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS tasks (
		id VARCHAR(255) PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		description TEXT,
		callback_url TEXT NOT NULL DEFAULT '',
		payload TEXT NOT NULL DEFAULT '',
		status VARCHAR(50) NOT NULL DEFAULT 'PENDING',
		retry_count INT NOT NULL DEFAULT 0,
		max_retries INT NOT NULL DEFAULT 3,
		execute_at TIMESTAMP WITH TIME ZONE NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_tasks_status_execute_at ON tasks (status, execute_at);

	CREATE TABLE IF NOT EXISTS dlq_tasks (
		id VARCHAR(255) PRIMARY KEY,
		task_id VARCHAR(255) NOT NULL,
		title VARCHAR(255),
		description TEXT,
		callback_url TEXT,
		payload TEXT,
		failed_at TIMESTAMP WITH TIME ZONE NOT NULL,
		retry_count INT NOT NULL
	);`
	if _, err := db.Exec(createTableQuery); err != nil {
		log.Printf("Warning: Table migration query failed: %v", err)
	}

	// 5. Initialize Repositories, Scheduler, and Handler
	repo := repository.NewPostgresRepo(db, rdb)
	dlqRepo := repository.NewPostgresDLQRepository(db)
	taskHandler := handler.NewHandler(repo)

	scheduler := service.NewTaskScheduler(repo, dlqRepo, cfg.WorkerCount, cfg.QueueCapacity)
	scheduler.Start(ctx)

	// 6. Setup routing with Gin
	router := gin.Default()
	taskHandler.RegisterRoutes(router, cfg.SchedulerAPIKey)

	// Register a mock handler to simulate client callback targets during testing
	router.POST("/mock-listener", func(c *gin.Context) {
		var payload map[string]interface{}
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("[Mock Listener] Received callback. Payload: %v", payload)
		c.JSON(http.StatusOK, gin.H{"status": "received"})
	})

	// 7. Start server with Graceful Shutdown
	port := ":8080"
	srv := &http.Server{
		Addr:    port,
		Handler: router,
	}

	go func() {
		fmt.Printf("Server is starting on port %s...\n", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for context cancellation (SIGINT/SIGTERM)
	<-ctx.Done()
	log.Println("Shutting down server and scheduler...")

	// Shut down scheduler first (waits for active workers to complete)
	scheduler.Stop()

	// Shut down HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP server shutdown failed: %v", err)
	}

	log.Println("Server exited gracefully.")
}
