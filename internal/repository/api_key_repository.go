package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApiKey represents a registered API key for a commercial client.
type ApiKey struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Key             string     `json:"key,omitempty"` // only returned on creation
	RateLimit       int        `json:"rate_limit"`
	RateLimitWindow int        `json:"rate_limit_window"`
	Quota           int        `json:"quota"`
	QuotaUsed       int        `json:"quota_used"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       time.Time  `json:"created_at"`
	LastUsedAt      *time.Time `json:"last_used_at"`
}

// ApiKeyRepository handles persistence for API keys.
type ApiKeyRepository struct {
	db *pgxpool.Pool
}

func NewApiKeyRepo(db *pgxpool.Pool) *ApiKeyRepository {
	return &ApiKeyRepository{db: db}
}

// Create inserts a new API key record.
func (r *ApiKeyRepository) Create(ctx context.Context, ak *ApiKey) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO api_keys (id, name, key, rate_limit, rate_limit_window, quota, quota_used, is_active, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		ak.ID, ak.Name, ak.Key, ak.RateLimit, ak.RateLimitWindow, ak.Quota, ak.QuotaUsed, ak.IsActive, ak.CreatedAt,
	)
	return err
}

// GetByKey fetches an API key by its key string. Returns nil if not found.
func (r *ApiKeyRepository) GetByKey(ctx context.Context, key string) (*ApiKey, error) {
	var ak ApiKey
	err := r.db.QueryRow(ctx,
		`SELECT id, name, key, rate_limit, rate_limit_window, quota, quota_used, is_active, created_at, last_used_at
		 FROM api_keys WHERE key = $1`, key,
	).Scan(&ak.ID, &ak.Name, &ak.Key, &ak.RateLimit, &ak.RateLimitWindow, &ak.Quota, &ak.QuotaUsed, &ak.IsActive, &ak.CreatedAt, &ak.LastUsedAt)
	if err != nil {
		return nil, err
	}
	return &ak, nil
}

// TouchLastUsed updates the last_used_at timestamp for a key.
func (r *ApiKeyRepository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

// IncrementQuota atomically bumps quota_used and returns the new value.
func (r *ApiKeyRepository) IncrementQuota(ctx context.Context, id uuid.UUID) (int, error) {
	var used int
	err := r.db.QueryRow(ctx,
		`UPDATE api_keys SET quota_used = quota_used + 1 WHERE id = $1 RETURNING quota_used`,
		id,
	).Scan(&used)
	return used, err
}

// List returns all API keys ordered by creation date.
func (r *ApiKeyRepository) List(ctx context.Context) ([]ApiKey, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, key, rate_limit, rate_limit_window, quota, quota_used, is_active, created_at, last_used_at
		 FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []ApiKey
	for rows.Next() {
		var ak ApiKey
		if err := rows.Scan(&ak.ID, &ak.Name, &ak.Key, &ak.RateLimit, &ak.RateLimitWindow, &ak.Quota, &ak.QuotaUsed, &ak.IsActive, &ak.CreatedAt, &ak.LastUsedAt); err != nil {
			return nil, err
		}
		keys = append(keys, ak)
	}
	return keys, nil
}

// GetByID fetches a single API key by its UUID.
func (r *ApiKeyRepository) GetByID(ctx context.Context, id uuid.UUID) (*ApiKey, error) {
	var ak ApiKey
	err := r.db.QueryRow(ctx,
		`SELECT id, name, key, rate_limit, rate_limit_window, quota, quota_used, is_active, created_at, last_used_at
		 FROM api_keys WHERE id = $1`, id,
	).Scan(&ak.ID, &ak.Name, &ak.Key, &ak.RateLimit, &ak.RateLimitWindow, &ak.Quota, &ak.QuotaUsed, &ak.IsActive, &ak.CreatedAt, &ak.LastUsedAt)
	if err != nil {
		return nil, err
	}
	return &ak, nil
}

// Update saves changes to an existing API key (name, rate limit, quota, active status).
func (r *ApiKeyRepository) Update(ctx context.Context, ak *ApiKey) error {
	_, err := r.db.Exec(ctx,
		`UPDATE api_keys SET name=$1, rate_limit=$2, rate_limit_window=$3, quota=$4, is_active=$5 WHERE id=$6`,
		ak.Name, ak.RateLimit, ak.RateLimitWindow, ak.Quota, ak.IsActive, ak.ID,
	)
	return err
}

// UpdateKey regenerates the key string for an existing record.
func (r *ApiKeyRepository) UpdateKey(ctx context.Context, id uuid.UUID, newKey string) error {
	_, err := r.db.Exec(ctx, `UPDATE api_keys SET key=$1 WHERE id=$2`, newKey, id)
	return err
}

// Delete removes an API key record.
func (r *ApiKeyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}
