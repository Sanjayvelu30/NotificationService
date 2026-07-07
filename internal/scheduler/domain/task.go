package domain

import "time"

type TaskStatus string

const (
	StatusPending    TaskStatus = "PENDING"
	StatusQueued     TaskStatus = "QUEUED"
	StatusProcessing TaskStatus = "PROCESSING"
	StatusCompleted  TaskStatus = "COMPLETED"
	StatusFailed     TaskStatus = "FAILED"
)

type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	CallbackURL string     `json:"callback_url"`
	Payload     string     `json:"payload"`
	Status      TaskStatus `json:"status"`
	RetryCount  int        `json:"retry_count"`
	MaxRetries  int        `json:"max_retries"`
	ExecuteAt   time.Time  `json:"execute_at"`
	CreatedAt   time.Time  `json:"created_at"`
}
