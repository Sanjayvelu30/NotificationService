package repository

import (
	"database/sql"
	"time"

	"github.com/sanjay/NotificationService/internal/scheduler/domain"
)

type postgresDLQRepository struct {
	db *sql.DB
}

// NewPostgresDLQRepository creates a new DLQRepo backed by PostgreSQL.
func NewPostgresDLQRepository(db *sql.DB) DLQRepo {
	return &postgresDLQRepository{db: db}
}

func (r *postgresDLQRepository) Save(task *domain.Task) error {
	query := `INSERT INTO dlq_tasks (id, task_id, title, description, callback_url, payload, failed_at, retry_count) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	
	// Create a unique entry ID for the DLQ table to prevent primary key conflicts if the task fails multiple times.
	dlqEntryID := task.ID + "_" + time.Now().Format("20060102150405")
	
	_, err := r.db.Exec(query, dlqEntryID, task.ID, task.Title, task.Description, task.CallbackURL, task.Payload, time.Now(), task.RetryCount)
	return err
}
