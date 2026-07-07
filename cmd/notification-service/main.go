package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sanjay/NotificationService/internal/config"
	"github.com/sanjay/NotificationService/internal/database"
	"github.com/sanjay/NotificationService/internal/domain"
	"github.com/sanjay/NotificationService/internal/handler"
	"github.com/sanjay/NotificationService/internal/middleware"
	"github.com/sanjay/NotificationService/internal/ratelimit"
	"github.com/sanjay/NotificationService/internal/repository/postgres"
	"github.com/sanjay/NotificationService/internal/router"
	"github.com/sanjay/NotificationService/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load configuration failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := database.RunMigrations(cfg.DatabaseURL); err != nil {
		slog.Error("run migrations", "error", err)
		os.Exit(1)
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := postgres.NewNotificationRepository(pool)
	dlqRepo := postgres.NewDLQRepository(pool)
	idempotencyRepo := postgres.NewInMemoryIdempotencyRepository()
	templateRepo := postgres.NewTemplateRepository(pool)

	// Initialize the Fixed Window Rate Limiter
	rlConfig := ratelimit.RateLimitConfig{
		MaxTokens: cfg.RateLimitMax,
		Duration:  cfg.RateLimitDuration,
	}
	rlRepo := ratelimit.NewInMemoryRateLimitRepo(rlConfig)
	rlSystem := ratelimit.NewRateLimitSystem(rlRepo)
	rlMiddleware := ratelimit.RateLimitMiddleware(rlSystem)

	// Initialize Auth0 authentication middleware
	auth0Middleware := middleware.NewAuth0Middleware(cfg.Auth0Domain)
	authMiddleware := auth0Middleware.Handler()

	system := &service.NotificationSystem{
		WorkerCount:         cfg.WorkerCount,
		MaxRetryCount:       3,
		Ctx:                 ctx,
		Cancel:              stop,
		NotificationChannel: make(chan domain.Notification, 256),
		NotificationRepo:    repo,
		TemplateRepo:        templateRepo,
		IdempotencyRepo:     idempotencyRepo,
		DLQRepo:             dlqRepo,
		NotificationStrategy: map[domain.NotificationType]service.NotificationSender{
			domain.Email: &service.EmailSender{
				TemplateRepo: templateRepo,
				ResendAPIKey: os.Getenv("RESEND_API_KEY"),
				EmailFrom:    cfg.EmailFrom,
				FailRate:     0.3,
			},
			domain.Sms:   &service.SmsSender{FailRate: 0.3},
			domain.Push:  &service.PushSender{FailRate: 0.3},
		},
	}

	// Start worker pool & retry scheduler
	system.Start()
	go system.RetryScheduler()

	notificationHandler := handler.NewNotificationHandler(system)
	templateHandler := handler.NewTemplateHandler(templateRepo)
	engine := router.New(notificationHandler, templateHandler, rlMiddleware, authMiddleware)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("starting HTTP server", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown", "error", err)
	}

	system.Shutdown()
	slog.Info("shutdown complete")
}
