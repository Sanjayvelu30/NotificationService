package repository

import (
	"context"
	"errors"
	"time"

	"github.com/sanjay/NotificationService/internal/scheduler/domain"
)

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskAlreadyExists = errors.New("task already exists")

// TaskRepo is the task storage repository interface.
type TaskRepo interface {
	Create(task *domain.Task) error
	Get(id string) (*domain.Task, error)
	Update(id string, updateFn func(t *domain.Task)) (*domain.Task, error)
	ClaimReadyTasks(now time.Time, limit int) ([]*domain.Task, error)
	RecoverTasks(ctx context.Context) error
}

// DLQRepo is the repository interface for handling Dead Letter Queue (DLQ) task logic.
type DLQRepo interface {
	Save(task *domain.Task) error
}
