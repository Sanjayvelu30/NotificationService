package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/sanjay/NotificationService/internal/scheduler/domain"
)

type postgresRepo struct {
	db       *sql.DB
	rdb      *redis.Client
	cacheTTL time.Duration
}

// NewPostgresRepo creates a new TaskRepo backed by PostgreSQL and Redis.
func NewPostgresRepo(db *sql.DB, rdb *redis.Client) TaskRepo {
	return &postgresRepo{
		db:       db,
		rdb:      rdb,
		cacheTTL: 10 * time.Minute,
	}
}

func (r *postgresRepo) cacheKey(id string) string {
	return "task:" + id
}

func (r *postgresRepo) Create(task *domain.Task) error {
	query := `INSERT INTO tasks (id, title, description, callback_url, payload, status, retry_count, max_retries, execute_at, created_at) 
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(query, task.ID, task.Title, task.Description, task.CallbackURL, task.Payload, task.Status, task.RetryCount, task.MaxRetries, task.ExecuteAt, task.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ErrTaskAlreadyExists
		}
		return err
	}
	return nil
}

func (r *postgresRepo) Get(id string) (*domain.Task, error) {
	ctx := context.Background()
	key := r.cacheKey(id)

	// 1. Try to get from Redis cache
	if r.rdb != nil {
		val, err := r.rdb.Get(ctx, key).Result()
		if err == nil {
			var task domain.Task
			if err := json.Unmarshal([]byte(val), &task); err == nil {
				log.Printf("[Repository] Cache HIT: task ID %s retrieved from Redis", id)
				return &task, nil
			}
		}
	}

	log.Printf("[Repository] Cache MISS: task ID %s not found in Redis. Querying PostgreSQL...", id)

	// 2. Cache miss: fetch from PostgreSQL
	var t domain.Task
	query := `SELECT id, title, description, callback_url, payload, status, retry_count, max_retries, execute_at, created_at 
	          FROM tasks WHERE id = $1`
	err := r.db.QueryRow(query, id).Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.CallbackURL,
		&t.Payload,
		&t.Status,
		&t.RetryCount,
		&t.MaxRetries,
		&t.ExecuteAt,
		&t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			log.Printf("[Repository] Database: task ID %s not found in PostgreSQL", id)
			return nil, ErrTaskNotFound
		}
		log.Printf("[Repository] Database error for task ID %s: %v", id, err)
		return nil, err
	}

	log.Printf("[Repository] Database HIT: task ID %s retrieved from PostgreSQL", id)

	// 3. Write back to Redis cache
	if r.rdb != nil {
		data, err := json.Marshal(t)
		if err == nil {
			if err := r.rdb.Set(ctx, key, data, r.cacheTTL).Err(); err != nil {
				log.Printf("[Repository] Cache write failed for task ID %s: %v", id, err)
			} else {
				log.Printf("[Repository] Cache write success for task ID %s", id)
			}
		}
	}

	return &t, nil
}

func (r *postgresRepo) Update(id string, updateFn func(t *domain.Task)) (*domain.Task, error) {
	ctx := context.Background()

	// Use a database transaction to ensure update consistency and row locking
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// 1. Get task from DB and lock the row
	var t domain.Task
	query := `SELECT id, title, description, callback_url, payload, status, retry_count, max_retries, execute_at, created_at 
	          FROM tasks WHERE id = $1 FOR UPDATE`
	err = tx.QueryRowContext(ctx, query, id).Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.CallbackURL,
		&t.Payload,
		&t.Status,
		&t.RetryCount,
		&t.MaxRetries,
		&t.ExecuteAt,
		&t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	// 2. Mutate state via callback
	updateFn(&t)

	// 3. Persist modifications back to DB
	updateQuery := `UPDATE tasks SET title = $1, description = $2, callback_url = $3, payload = $4, status = $5, retry_count = $6, max_retries = $7, execute_at = $8 
	                WHERE id = $9`
	_, err = tx.ExecContext(ctx, updateQuery, t.Title, t.Description, t.CallbackURL, t.Payload, t.Status, t.RetryCount, t.MaxRetries, t.ExecuteAt, t.ID)
	if err != nil {
		return nil, err
	}

	// 4. Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// 5. Invalidate Redis cache
	if r.rdb != nil {
		key := r.cacheKey(id)
		_ = r.rdb.Del(ctx, key).Err()
	}

	return &t, nil
}

func (r *postgresRepo) ClaimReadyTasks(now time.Time, limit int) ([]*domain.Task, error) {
	// Atomically select PENDING tasks that are ready to run, update status to QUEUED,
	// and return the records using standard FOR UPDATE SKIP LOCKED to prevent duplicate execution.
	query := `
		UPDATE tasks
		SET status = 'QUEUED'
		WHERE id IN (
			SELECT id FROM tasks
			WHERE status = 'PENDING' AND execute_at <= $1
			ORDER BY execute_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, title, description, callback_url, payload, status, retry_count, max_retries, execute_at, created_at`

	rows, err := r.db.Query(query, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		var t domain.Task
		err := rows.Scan(
			&t.ID,
			&t.Title,
			&t.Description,
			&t.CallbackURL,
			&t.Payload,
			&t.Status,
			&t.RetryCount,
			&t.MaxRetries,
			&t.ExecuteAt,
			&t.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *postgresRepo) RecoverTasks(ctx context.Context) error {
	// Revert any tasks stuck in QUEUED or PROCESSING on startup
	query := `
		UPDATE tasks
		SET status = 'PENDING'
		WHERE status IN ('QUEUED', 'PROCESSING')`

	res, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("[Repository] Recovered %d tasks stuck in QUEUED/PROCESSING status back to PENDING.", rowsAffected)
	}
	return nil
}
