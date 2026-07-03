package handler

import (
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
}

type notificationResponse struct {
	ID          string            `json:"id"`
	Recipient   string            `json:"recipient"`
	Template    string            `json:"template"`
	Variable    map[string]string `json:"variable"`
	RetryCount  int               `json:"retry_count"`
	CreatedAt   string            `json:"created_at"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	NextRetryAt string            `json:"next_retry_at"`
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

	notification, err := h.system.CreateNotification(
		strings.TrimSpace(req.Template),
		req.Variable,
		nType,
		strings.TrimSpace(req.Recipient),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, toResponse(notification))
}

func (h *NotificationHandler) GetByID(c *gin.Context) {
	id := c.Param("id")
	
	// Check the standard repository
	notification, err := h.system.NotificationRepo.Get(id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			// Fallback check the DLQ Repository
			dlqNotification, errDlq := h.system.DLQRepo.Get(id)
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
		ID:          n.ID,
		Recipient:   n.Recipient,
		Template:    n.Template,
		Variable:    n.Variable,
		RetryCount:  n.RetryCount,
		CreatedAt:   n.CreatedAt.UTC().Format(time.RFC3339),
		Type:        string(n.Type),
		Status:      string(n.Status),
		NextRetryAt: n.NextRetryAt.UTC().Format(time.RFC3339),
	}
}
