package distribution

import (
	"context"
	"database/sql"
	"fmt"

	"myAgent/pkg/model"
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
func (r *Repository) InsertResult(ctx context.Context, pr *model.PostResult) error {
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

// ResultsByJobID returns all PostResults for a given job ordered by creation time.
func (r *Repository) ResultsByJobID(ctx context.Context, jobID string) ([]model.PostResult, error) {
	const q = `
		SELECT id, job_id, user_id, platform, status, platform_post_id,
		       platform_url, error_detail, attempt_count, created_at
		FROM post_results
		WHERE job_id = ?
		ORDER BY created_at`

	rows, err := r.db.QueryContext(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("distribution repo: query results: %w", err)
	}
	defer rows.Close()

	var results []model.PostResult
	for rows.Next() {
		var pr model.PostResult
		if err := rows.Scan(
			&pr.ID, &pr.JobID, &pr.UserID, &pr.Platform, &pr.Status,
			&pr.PlatformPostID, &pr.PlatformURL, &pr.ErrorDetail,
			&pr.AttemptCount, &pr.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("distribution repo: scan result: %w", err)
		}
		results = append(results, pr)
	}
	return results, rows.Err()
}
