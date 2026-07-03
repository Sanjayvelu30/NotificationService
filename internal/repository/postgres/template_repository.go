package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TemplateRepository struct {
	pool *pgxpool.Pool
}

func NewTemplateRepository(pool *pgxpool.Pool) *TemplateRepository {
	return &TemplateRepository{pool: pool}
}

func (r *TemplateRepository) Save(name string, body string, userID string) error {
	if userID == "" {
		userID = "global"
	}
	query := `
		INSERT INTO templates (name, body, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, name) DO UPDATE SET body = EXCLUDED.body`

	_, err := r.pool.Exec(context.Background(), query, name, body, userID)
	if err != nil {
		return fmt.Errorf("save template: %w", err)
	}
	return nil
}

func (r *TemplateRepository) Get(name string, userID string) (string, error) {
	if userID == "" {
		userID = "global"
	}
	query := `
		SELECT body FROM templates 
		WHERE name = $1 AND (user_id = $2 OR user_id = 'global')
		ORDER BY (user_id = 'global') ASC, user_id DESC
		LIMIT 1`

	var body string
	err := r.pool.QueryRow(context.Background(), query, name, userID).Scan(&body)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", fmt.Errorf("template not found: %s", name)
		}
		return "", fmt.Errorf("get template: %w", err)
	}
	return body, nil
}

func (r *TemplateRepository) GetAll(userID string) (map[string]string, error) {
	if userID == "" {
		userID = "global"
	}
	query := `
		SELECT DISTINCT ON (name) name, body 
		FROM templates 
		WHERE user_id = $1 OR user_id = 'global'
		ORDER BY name, (user_id = 'global') ASC, user_id DESC`

	rows, err := r.pool.Query(context.Background(), query, userID)
	if err != nil {
		return nil, fmt.Errorf("query all templates: %w", err)
	}
	defer rows.Close()

	templates := make(map[string]string)
	for rows.Next() {
		var name, body string
		if err := rows.Scan(&name, &body); err != nil {
			return nil, fmt.Errorf("scan template row: %w", err)
		}
		templates[name] = body
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate templates: %w", err)
	}

	return templates, nil
}
