package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanjay/NotificationService/internal/scheduler/domain"
	"github.com/sanjay/NotificationService/internal/scheduler/middleware"
	"github.com/sanjay/NotificationService/internal/scheduler/repository"
)

type Handler struct {
	repo repository.TaskRepo
}

func NewHandler(repo repository.TaskRepo) *Handler {
	return &Handler{repo: repo}
}

func (h *Handler) CreateTask(c *gin.Context) {
	var t domain.Task
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Idempotency: If client provides an ID, use it; otherwise generate one.
	if t.ID == "" {
		t.ID = generateID()
	}
	t.CreatedAt = time.Now()
	t.Status = domain.StatusPending // Enforce StatusPending unconditionally
	t.RetryCount = 0

	// Set defaults if empty
	if t.ExecuteAt.IsZero() {
		t.ExecuteAt = t.CreatedAt
	}
	if t.MaxRetries <= 0 {
		t.MaxRetries = 3
	}

	if err := h.repo.Create(&t); err != nil {
		if errors.Is(err, repository.ErrTaskAlreadyExists) {
			// Idempotent: return existing task
			existingTask, getErr := h.repo.Get(t.ID)
			if getErr == nil {
				c.JSON(http.StatusOK, existingTask)
				return
			}
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, t)
}

func (h *Handler) GetTask(c *gin.Context) {
	id := c.Param("id")
	t, err := h.repo.Get(id)
	if err != nil {
		if errors.Is(err, repository.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, t)
}

func (h *Handler) UpdateTask(c *gin.Context) {
	id := c.Param("id")

	var updatePayload struct {
		Title       *string    `json:"title"`
		Description *string    `json:"description"`
		ExecuteAt   *time.Time `json:"execute_at"`
	}

	if err := c.ShouldBindJSON(&updatePayload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	t, err := h.repo.Update(id, func(task *domain.Task) {
		if updatePayload.Title != nil {
			task.Title = *updatePayload.Title
		}
		if updatePayload.Description != nil {
			task.Description = *updatePayload.Description
		}
		if updatePayload.ExecuteAt != nil {
			task.ExecuteAt = *updatePayload.ExecuteAt
			// Revert task back to PENDING if rescheduled to execute in the future
			task.Status = domain.StatusPending
			task.RetryCount = 0
		}
	})

	if err != nil {
		if errors.Is(err, repository.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, t)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func (h *Handler) RegisterRoutes(router *gin.Engine, apiKey string) {
	router.GET("/health", h.Health)
	tasksGroup := router.Group("/tasks")
	if apiKey != "" {
		tasksGroup.Use(middleware.APIKeyAuth(apiKey))
	}
	{
		tasksGroup.POST("", h.CreateTask)
		tasksGroup.GET("/:id", h.GetTask)
		tasksGroup.PUT("/:id", h.UpdateTask)
	}
}

func generateID() string {
	return time.Now().Format("20060102150405")
}
