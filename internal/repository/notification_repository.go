package repository

import (
	"github.com/sanjay/NotificationService/internal/domain"
)

type NotificationRepo interface {
	Save(domain.Notification) error
	Update(domain.Notification) error
	Get(string) (domain.Notification, error)
	GetReadyJobs() ([]domain.Notification, error)
}

type DLQRepo interface {
	Save(domain.Notification) error
	Get(string) (domain.Notification, error)
}

type IdempotencyRepo interface {
	Exisits(key string) (any, error)
	Save(key string) error
	Update(key string, value any) error
}
