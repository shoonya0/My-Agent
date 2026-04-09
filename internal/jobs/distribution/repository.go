package distribution

import (
	"context"
	"database/sql"
	"fmt"

	"myAgent/pkg/types"
)

// Repository persists PostResult records to MySQL.
type Repository struct {
	db *sql.DB
}

// NewRepository creates a distribution Repository backed by the given database.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// InsertResult records a single platform posting outcome.
func (r *Repository) InsertResult(ctx context.Context, pr *types.PostResult) error {
	const q = `
		INSERT INTO post_results
			(id, job_id, user_id, platform, status, platform_post_id,
			 platform_url, error_detail, attempt_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())`

	_, err := r.db.ExecContext(ctx, q,
		pr.ID, pr.JobID, pr.UserID, pr.Platform, pr.Status,
		pr.PlatformPostID, pr.PlatformURL, pr.ErrorDetail, pr.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("distribution repo: insert result: %w", err)
	}
	return nil
}
