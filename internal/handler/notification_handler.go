package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/domain"
	"github.com/sanjay/NotificationService/internal/service"
)

type NotificationHandler struct {
	system *service.NotificationSystem
}

func NewNotificationHandler(system *service.NotificationSystem) *NotificationHandler {
	return &NotificationHandler{system: system}
}

type createNotificationRequest struct {
	Recipient string            `json:"recipient" binding:"required"`
	Template  string            `json:"template" binding:"required"`
	Variable  map[string]string `json:"variable"`
	Type      string            `json:"type" binding:"required"`
	ExecuteAt string            `json:"execute_at"`
}

type notificationResponse struct {
	ID           string            `json:"id"`
	Recipient    string            `json:"recipient"`
	Template     string            `json:"template"`
	Variable     map[string]string `json:"variable"`
	RetryCount   int               `json:"retry_count"`
	CreatedAt    string            `json:"created_at"`
	Type         string            `json:"type"`
	Status       string            `json:"status"`
	NextRetryAt  string            `json:"next_retry_at"`
	ErrorMessage string            `json:"error_message"`
}

func (h *NotificationHandler) Create(c *gin.Context) {
	var req createNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	nType := domain.NotificationType(strings.ToUpper(strings.TrimSpace(req.Type)))
	if nType != domain.Email && nType != domain.Sms && nType != domain.Push {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification type: must be EMAIL, SMS, or PUSH"})
		return
	}

	var executeTime time.Time
	if req.ExecuteAt != "" {
		var err error
		executeTime, err = time.Parse(time.RFC3339, req.ExecuteAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execute_at timestamp: must be RFC3339 format"})
			return
		}
		if executeTime.Before(time.Now()) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "execute_at must be in the future"})
			return
		}
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	notification, err := h.system.CreateNotification(
		strings.TrimSpace(req.Template),
		req.Variable,
		nType,
		strings.TrimSpace(req.Recipient),
		userIDStr,
		executeTime,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !executeTime.IsZero() {
		if err := h.system.ScheduleNotification(notification, executeTime); err != nil {
			// Clean up saved scheduled record on delegate error
			notification.Status = domain.DLQ
			notification.ErrorMessage = "failed to schedule task: " + err.Error()
			_ = h.system.NotificationRepo.Update(notification)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delegate scheduled task: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusAccepted, toResponse(notification))
}

func (h *NotificationHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	
	// Check the standard repository
	notification, err := h.system.NotificationRepo.Get(id, userIDStr)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// Fallback check the DLQ Repository
			dlqNotification, errDlq := h.system.DLQRepo.Get(id, userIDStr)
			if errDlq == nil {
				c.JSON(http.StatusOK, toResponse(dlqNotification))
				return
			}
			c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, toResponse(notification))
}

func toResponse(n domain.Notification) notificationResponse {
	return notificationResponse{
		ID:           n.ID,
		Recipient:    n.Recipient,
		Template:     n.Template,
		Variable:     n.Variable,
		RetryCount:   n.RetryCount,
		CreatedAt:    n.CreatedAt.UTC().Format(time.RFC3339),
		Type:         string(n.Type),
		Status:       string(n.Status),
		NextRetryAt:  n.NextRetryAt.UTC().Format(time.RFC3339),
		ErrorMessage: n.ErrorMessage,
	}
}

func (h *NotificationHandler) DispatchCallback(c *gin.Context) {
	// Validate authorization token header matches SCHEDULER_API_KEY
	authHeader := c.GetHeader("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" || parts[1] != h.system.SchedulerAPIKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized callback access"})
		return
	}

	id := c.Param("id")
	// Pull the notification using the "system" bypass token
	notification, err := h.system.NotificationRepo.Get(id, "system")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification record not found"})
		return
	}

	if notification.Status != domain.Scheduled {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("notification is in %s state; must be SCHEDULED to trigger", notification.Status)})
		return
	}

	// Transition status to PENDING and trigger immediately
	notification.Status = domain.Pending
	notification.NextRetryAt = time.Now()

	if err := h.system.NotificationRepo.Update(notification); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update notification: " + err.Error()})
		return
	}

	// Push directly to the background processing channel queue
	select {
	case h.system.NotificationChannel <- notification:
		c.JSON(http.StatusOK, gin.H{"status": "dispatched"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system channels saturated; dispatch deferred to poller"})
	}
}
