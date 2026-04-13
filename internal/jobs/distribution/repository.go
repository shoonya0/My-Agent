package distribution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const otelScopeRepository = "internal/jobs/distribution"

// ErrJobNotFound is returned when no job row matches the given ID.
var ErrJobNotFound = errors.New("distribution: job not found")

// Repository persists PostResult records and job status updates to MySQL.
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

// UpdateJobStatus sets the job row status (e.g. completed after distribution).
func (r *Repository) UpdateJobStatus(ctx context.Context, jobID, status string) error {
	ctx, span := otel.Tracer(otelScopeRepository).Start(ctx, "distribution.Repository.UpdateJobStatus")
	defer span.End()

	span.SetAttributes(
		attribute.String("db.system", "mysql"),
		attribute.String("db.operation", "update"),
		attribute.String("job.id", jobID),
		attribute.String("job.status", status),
	)

	now := time.Now()
	const q = `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q, status, now, jobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update job status")
		return fmt.Errorf("distribution repo: update job status: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rows affected")
		return fmt.Errorf("distribution repo: rows affected: %w", err)
	}
	if n == 0 {
		err := ErrJobNotFound
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
