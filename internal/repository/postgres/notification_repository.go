package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sanjay/NotificationService/internal/domain"
)

type NotificationRepository struct {
	pool *pgxpool.Pool
}

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	return &NotificationRepository{pool: pool}
}

func (r *NotificationRepository) Save(n domain.Notification) error {
	varBytes, err := json.Marshal(n.Variable)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}

	query := `
		INSERT INTO notifications (
			id, recipient, template, variable, retry_count,
			created_at, type, status, next_retry_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9
		)`

	_, err = r.pool.Exec(context.Background(), query,
		n.ID, n.Recipient, n.Template, varBytes, n.RetryCount,
		n.CreatedAt, string(n.Type), string(n.Status), n.NextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("save notification: %w", err)
	}
	return nil
}

func (r *NotificationRepository) Update(n domain.Notification) error {
	varBytes, err := json.Marshal(n.Variable)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}

	query := `
		UPDATE notifications
		SET recipient = $2, template = $3, variable = $4, retry_count = $5,
		    created_at = $6, type = $7, status = $8, next_retry_at = $9
		WHERE id = $1`

	_, err = r.pool.Exec(context.Background(), query,
		n.ID, n.Recipient, n.Template, varBytes, n.RetryCount,
		n.CreatedAt, string(n.Type), string(n.Status), n.NextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("update notification: %w", err)
	}
	return nil
}

func (r *NotificationRepository) Get(id string) (domain.Notification, error) {
	query := `
		SELECT id, recipient, template, variable, retry_count,
		       created_at, type, status, next_retry_at
		FROM notifications
		WHERE id = $1`

	var n domain.Notification
	var varBytes []byte
	var nType, nStatus string

	err := r.pool.QueryRow(context.Background(), query, id).Scan(
		&n.ID, &n.Recipient, &n.Template, &varBytes, &n.RetryCount,
		&n.CreatedAt, &nType, &nStatus, &n.NextRetryAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Notification{}, fmt.Errorf("notification not found")
		}
		return domain.Notification{}, fmt.Errorf("get notification: %w", err)
	}

	n.Type = domain.NotificationType(nType)
	n.Status = domain.NotificationStatus(nStatus)

	if err := json.Unmarshal(varBytes, &n.Variable); err != nil {
		return domain.Notification{}, fmt.Errorf("unmarshal variables: %w", err)
	}

	return n, nil
}

func (r *NotificationRepository) GetReadyJobs() ([]domain.Notification, error) {
	// Atomically updates pending notifications ready to retry/send to PROCESSING,
	// using FOR UPDATE SKIP LOCKED to prevent race conditions.
	query := `
		UPDATE notifications
		SET status = 'PROCESSING'
		WHERE id IN (
			SELECT id FROM notifications
			WHERE status = 'PENDING' AND next_retry_at <= NOW()
			ORDER BY created_at ASC
			LIMIT 50
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, recipient, template, variable, retry_count, created_at, type, status, next_retry_at`

	rows, err := r.pool.Query(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("query ready jobs: %w", err)
	}
	defer rows.Close()

	var notifications []domain.Notification
	for rows.Next() {
		var n domain.Notification
		var varBytes []byte
		var nType, nStatus string

		err := rows.Scan(
			&n.ID, &n.Recipient, &n.Template, &varBytes, &n.RetryCount,
			&n.CreatedAt, &nType, &nStatus, &n.NextRetryAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan ready job: %w", err)
		}

		n.Type = domain.NotificationType(nType)
		n.Status = domain.NotificationStatus(nStatus)

		if err := json.Unmarshal(varBytes, &n.Variable); err != nil {
			return nil, fmt.Errorf("unmarshal variables: %w", err)
		}

		notifications = append(notifications, n)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ready jobs: %w", err)
	}

	return notifications, nil
}

type DLQRepository struct {
	pool *pgxpool.Pool
}

func NewDLQRepository(pool *pgxpool.Pool) *DLQRepository {
	return &DLQRepository{pool: pool}
}

func (r *DLQRepository) Save(n domain.Notification) error {
	varBytes, err := json.Marshal(n.Variable)
	if err != nil {
		return fmt.Errorf("marshal variables: %w", err)
	}

	query := `
		INSERT INTO dlq_notifications (
			id, recipient, template, variable, retry_count,
			created_at, type, status, next_retry_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9
		)`

	_, err = r.pool.Exec(context.Background(), query,
		n.ID, n.Recipient, n.Template, varBytes, n.RetryCount,
		n.CreatedAt, string(n.Type), string(n.Status), n.NextRetryAt,
	)
	if err != nil {
		return fmt.Errorf("save dlq notification: %w", err)
	}
	return nil
}

func (r *DLQRepository) Get(id string) (domain.Notification, error) {
	query := `
		SELECT id, recipient, template, variable, retry_count,
		       created_at, type, status, next_retry_at
		FROM dlq_notifications
		WHERE id = $1`

	var n domain.Notification
	var varBytes []byte
	var nType, nStatus string

	err := r.pool.QueryRow(context.Background(), query, id).Scan(
		&n.ID, &n.Recipient, &n.Template, &varBytes, &n.RetryCount,
		&n.CreatedAt, &nType, &nStatus, &n.NextRetryAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Notification{}, fmt.Errorf("dlq notification not found")
		}
		return domain.Notification{}, fmt.Errorf("get dlq notification: %w", err)
	}

	n.Type = domain.NotificationType(nType)
	n.Status = domain.NotificationStatus(nStatus)

	if err := json.Unmarshal(varBytes, &n.Variable); err != nil {
		return domain.Notification{}, fmt.Errorf("unmarshal variables: %w", err)
	}

	return n, nil
}

type InMemoryIdempotencyRepository struct {
	mu   sync.Mutex
	keys map[string]any
}

func NewInMemoryIdempotencyRepository() *InMemoryIdempotencyRepository {
	return &InMemoryIdempotencyRepository{
		keys: make(map[string]any),
	}
}

func (r *InMemoryIdempotencyRepository) Exisits(key string) (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	response, exist := r.keys[key]
	if !exist {
		return nil, fmt.Errorf("not found")
	}
	return response, nil
}

func (r *InMemoryIdempotencyRepository) Save(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key] = true
	return nil
}

func (r *InMemoryIdempotencyRepository) Update(key string, value any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key] = value
	return nil
}
