package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
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
	FailRate float64
}

func (e *EmailSender) Send(notification domain.Notification) error {
	slog.Info("attempting to send email notification", "id", notification.ID, "recipient", notification.Recipient)
	if notification.Recipient == "fail@example.com" {
		return fmt.Errorf("permanent email sending failure for testing")
	}
	if e.FailRate > 0 && rand.Float64() < e.FailRate {
		return fmt.Errorf("transient email delivery failure")
	}
	slog.Info("email notification sent successfully", "id", notification.ID)
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
	IdempotencyRepo      repository.IdempotencyRepo
	Wg                   sync.WaitGroup
	NotificationStrategy map[domain.NotificationType]NotificationSender
	QueueSize            int
	ShuttingDown         atomic.Bool
	DLQRepo              repository.DLQRepo
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
		
		// retry less than limit, schedule next attempt
		backoff := time.Second * time.Duration(1<<notification.RetryCount)
		notification.NextRetryAt = time.Now().Add(backoff)
		notification.Status = domain.Pending // Set back to PENDING so scheduler can pick it up

		if err := n.NotificationRepo.Update(notification); err != nil {
			slog.Error("failed to update status for retry scheduling", "id", notification.ID, "error", err)
		}
		return
	}

	// Succeeded
	notification.Status = domain.Sent
	if err := n.NotificationRepo.Update(notification); err != nil {
		slog.Error("failed to update status to sent", "id", notification.ID, "error", err)
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

func (n *NotificationSystem) CreateNotification(templateName string, variable map[string]string, notificationType domain.NotificationType, recipient string) (domain.Notification, error) {
	if n.ShuttingDown.Load() {
		return domain.Notification{}, fmt.Errorf("notification system is shutting down")
	}

	// Resolve/check template exists
	if _, exists := domain.Templates[templateName]; !exists {
		return domain.Notification{}, fmt.Errorf("invalid template name: %s", templateName)
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
		Status:      domain.Pending,
		NextRetryAt: time.Now(), // Process immediately
	}

	if err := n.NotificationRepo.Save(notification); err != nil {
		return domain.Notification{}, err
	}

	select {
	case n.NotificationChannel <- notification:
		return notification, nil
	case <-n.Ctx.Done():
		return domain.Notification{}, fmt.Errorf("notification system stopped")
	}
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
