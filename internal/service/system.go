package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/sanjay/NotificationService/internal/domain"
	"github.com/sanjay/NotificationService/internal/repository"
)

type NotificationSender interface {
	Send(domain.Notification) error
}

type EmailSender struct {
	TemplateRepo repository.TemplateRepo
	ResendAPIKey string
	EmailFrom    string
	FailRate     float64
}

func (e *EmailSender) Send(notification domain.Notification) error {
	slog.Info("attempting to send email notification", "id", notification.ID, "recipient", notification.Recipient)
	if notification.Recipient == "fail@example.com" {
		return fmt.Errorf("permanent email sending failure for testing")
	}

	// Retrieve the template body from the database repository (matching user_id or global fallback)
	templateBody, err := e.TemplateRepo.Get(notification.Template, notification.UserID)
	if err != nil {
		return fmt.Errorf("load template %q: %w", notification.Template, err)
	}

	// Dynamic placeholder rendering (e.g. {{name}} -> User)
	body := templateBody
	for k, v := range notification.Variable {
		body = strings.ReplaceAll(body, "{{"+k+"}}", v)
	}

	// If no API Key is provided, fallback to logging the rendered template
	if e.ResendAPIKey == "" {
		slog.Warn("RESEND_API_KEY is empty, falling back to mock logger success", "id", notification.ID)
		if e.FailRate > 0 && rand.Float64() < e.FailRate {
			return fmt.Errorf("transient email delivery failure (mock)")
		}
		slog.Info("mock email notification sent successfully", "id", notification.ID, "body", body)
		return nil
	}

	// Construct payload for Resend REST API
	fromAddress := e.EmailFrom
	if fromAddress == "" {
		fromAddress = "no-reply@sanjayvelu.online"
	}

	payload := map[string]any{
		"from":    fromAddress,
		"to":      []string{notification.Recipient},
		"subject": fmt.Sprintf("Notification: %s", notification.Template),
		"html":    fmt.Sprintf("<p>%s</p>", body),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal resend payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.ResendAPIKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		var errMap map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errMap)
		return fmt.Errorf("resend API returned error (status %d): %v", resp.StatusCode, errMap)
	}

	slog.Info("email notification sent successfully via Resend API", "id", notification.ID)
	return nil
}

type PushSender struct {
	FailRate float64
}

func (p *PushSender) Send(notification domain.Notification) error {
	slog.Info("attempting to send push notification", "id", notification.ID, "recipient", notification.Recipient)
	if p.FailRate > 0 && rand.Float64() < p.FailRate {
		return fmt.Errorf("transient push delivery failure")
	}
	slog.Info("push notification sent successfully", "id", notification.ID)
	return nil
}

type SmsSender struct {
	FailRate float64
}

func (s *SmsSender) Send(notification domain.Notification) error {
	slog.Info("attempting to send SMS notification", "id", notification.ID, "recipient", notification.Recipient)
	if s.FailRate > 0 && rand.Float64() < s.FailRate {
		return fmt.Errorf("transient SMS delivery failure")
	}
	slog.Info("SMS notification sent successfully", "id", notification.ID)
	return nil
}

type NotificationSystem struct {
	WorkerCount          int
	MaxRetryCount        int
	Ctx                  context.Context
	Cancel               context.CancelFunc
	NotificationChannel  chan domain.Notification
	NotificationRepo     repository.NotificationRepo
	TemplateRepo         repository.TemplateRepo
	IdempotencyRepo      repository.IdempotencyRepo
	Wg                   sync.WaitGroup
	NotificationStrategy map[domain.NotificationType]NotificationSender
	QueueSize            int
	ShuttingDown         atomic.Bool
	DLQRepo              repository.DLQRepo
	SchedulerURL         string
	SchedulerAPIKey      string
}

func (n *NotificationSystem) Process(notification domain.Notification) {
	// Update Status to Processing in database
	notification.Status = domain.Processing
	if err := n.NotificationRepo.Update(notification); err != nil {
		slog.Error("failed to update status to processing", "id", notification.ID, "error", err)
	}

	// Call sender
	err := n.Job(notification)
	if err != nil {
		slog.Warn("job execution failed", "id", notification.ID, "error", err)
		// Increase retry Count
		notification.RetryCount++
		notification.ErrorMessage = err.Error()
		if notification.RetryCount > n.MaxRetryCount {
			notification.Status = domain.DLQ
			if err := n.NotificationRepo.Update(notification); err != nil {
				slog.Error("failed to update status to DLQ", "id", notification.ID, "error", err)
			}
			if err := n.DLQRepo.Save(notification); err != nil {
				slog.Error("failed to save to DLQ repo", "id", notification.ID, "error", err)
			}
			return
		}

		// Update next retry time with exponential backoff (2^retry_count seconds)
		delay := time.Duration(1<<notification.RetryCount) * time.Second
		notification.NextRetryAt = time.Now().Add(delay)
		notification.Status = domain.Pending

		if err := n.NotificationRepo.Update(notification); err != nil {
			slog.Error("failed to update status for retry", "id", notification.ID, "error", err)
		}
		return
	}

	// Success
	notification.Status = domain.Sent
	notification.ErrorMessage = ""
	if err := n.NotificationRepo.Update(notification); err != nil {
		slog.Error("failed to update status to SENT", "id", notification.ID, "error", err)
	}
}

func (n *NotificationSystem) Job(notification domain.Notification) error {
	sender, exist := n.NotificationStrategy[notification.Type]
	if !exist {
		return fmt.Errorf("strategy sender not found for type: %s", notification.Type)
	}
	return sender.Send(notification)
}

func (n *NotificationSystem) Worker() {
	defer n.Wg.Done()
	for {
		select {
		case <-n.Ctx.Done():
			slog.Info("worker shutting down")
			return
		case notification, ok := <-n.NotificationChannel:
			if !ok {
				slog.Info("worker channel closed, shutting down")
				return
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("recovered from panic inside worker", "panic", r)
					}
				}()
				n.Process(notification)
			}()
		}
	}
}

func (n *NotificationSystem) Start() {
	for i := 0; i < n.WorkerCount; i++ {
		n.Wg.Add(1)
		go n.Worker()
	}
	slog.Info("worker pool started", "workers", n.WorkerCount)
}

func (n *NotificationSystem) CreateNotification(templateName string, variable map[string]string, notificationType domain.NotificationType, recipient string, userID string, executeAt time.Time) (domain.Notification, error) {
	if n.ShuttingDown.Load() {
		return domain.Notification{}, fmt.Errorf("notification system is shutting down")
	}

	// 1. Enforce system threshold of 100 max notifications
	totalCount, err := n.NotificationRepo.GetTotalCount()
	if err != nil {
		return domain.Notification{}, fmt.Errorf("failed to check system threshold: %w", err)
	}
	if totalCount >= 100 {
		return domain.Notification{}, fmt.Errorf("system threshold exceeded: maximum limit of 100 total notifications reached")
	}

	// 2. Resolve/check template exists in database
	_, err = n.TemplateRepo.Get(templateName, userID)
	if err != nil {
		return domain.Notification{}, fmt.Errorf("invalid template name: %w", err)
	}

	status := domain.Pending
	nextRetry := time.Now()
	if !executeAt.IsZero() {
		status = domain.Scheduled
		nextRetry = executeAt
	}

	id := uuid.NewString()
	notification := domain.Notification{
		ID:          id,
		Recipient:   recipient,
		Template:    templateName,
		Variable:    variable,
		RetryCount:  0,
		CreatedAt:   time.Now(),
		Type:        notificationType,
		Status:      status,
		NextRetryAt: nextRetry,
		UserID:      userID,
	}

	if err := n.NotificationRepo.Save(notification); err != nil {
		return domain.Notification{}, err
	}

	// Only enqueue immediately if it's not scheduled for a future date/time
	if executeAt.IsZero() {
		select {
		case n.NotificationChannel <- notification:
			return notification, nil
		case <-n.Ctx.Done():
			return domain.Notification{}, fmt.Errorf("notification system stopped")
		}
	}

	return notification, nil
}

func (n *NotificationSystem) Shutdown() {
	n.ShuttingDown.Store(true)
	n.Cancel()
	n.Wg.Wait()
	close(n.NotificationChannel)
}

func (n *NotificationSystem) RetryScheduler() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.Ctx.Done():
			slog.Info("retry scheduler shutting down")
			return
		case <-ticker.C:
			jobs, err := n.NotificationRepo.GetReadyJobs()
			if err != nil {
				slog.Error("scheduler: failed to fetch ready jobs", "error", err)
				continue
			}
			for _, job := range jobs {
				if n.ShuttingDown.Load() {
					return
				}
				select {
				case n.NotificationChannel <- job:
					slog.Info("scheduler: enqueued ready job", "id", job.ID)
				case <-n.Ctx.Done():
					return
				}
			}
		}
	}
}

func (n *NotificationSystem) ScheduleNotification(notification domain.Notification, executeAt time.Time) error {
	slog.Info("delegating scheduled notification to Task Scheduler microservice", "id", notification.ID, "executeAt", executeAt)

	// Callback URL points back to our internal notifications callback endpoint
	callbackURL := "http://notification-service:8080/api/v1/internal/notifications/" + notification.ID + "/dispatch"

	taskPayload := map[string]any{
		"id":           "task_notif_" + notification.ID,
		"title":        "Trigger Notification Dispatch: " + notification.ID,
		"description":  "Callback trigger enqueuing notification for " + notification.Recipient,
		"callback_url": callbackURL,
		"payload":      "{}", // Task Scheduler doesn't need to pass parameters back since we pull by ID
		"execute_at":   executeAt.Format(time.RFC3339),
		"max_retries":  3,
	}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		return fmt.Errorf("marshal scheduler task payload: %w", err)
	}

	req, err := http.NewRequest("POST", n.SchedulerURL+"/tasks", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("create scheduler HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if n.SchedulerAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.SchedulerAPIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute scheduler HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errMap map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errMap)
		return fmt.Errorf("scheduler returned non-2xx status code %d: %v", resp.StatusCode, errMap)
	}

	slog.Info("successfully scheduled task with Task Scheduler", "id", notification.ID)
	return nil
}
