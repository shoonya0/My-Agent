package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/model"
)

// ErrJobNotFound is returned when no job matches the given ID.
var ErrJobNotFound = errors.New("orchestrator: job not found")

// Repository defines the data-access contract for job records.
type Repository interface {
	CreateJob(ctx context.Context, job *model.Job) error
	GetJobByID(ctx context.Context, id string) (*model.Job, error)
	UpdateJobStatus(ctx context.Context, id, status string) error
	UpdateJob(ctx context.Context, job *model.Job) error
	InsertStatusHistory(ctx context.Context, h *model.JobStatusHistory) error
}

type mysqlRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by a MySQL database connection.
func NewRepository(db *sql.DB) Repository {
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) CreateJob(ctx context.Context, job *model.Job) error {
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now

	const q = `
		INSERT INTO jobs (id, user_id, status, original_prompt, refined_prompt,
			original_image_url, generated_image_url, execution_plan, error_message,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		job.ID, job.UserID, job.Status, job.OriginalPrompt, job.RefinedPrompt,
		job.OriginalImageURL, job.GeneratedImageURL, job.ExecutionPlan,
		job.ErrorMessage, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("orchestrator: create job: %w", err)
	}
	return nil
}

func (r *mysqlRepository) GetJobByID(ctx context.Context, id string) (*model.Job, error) {
	const q = `
		SELECT id, user_id, status, original_prompt, refined_prompt,
			original_image_url, generated_image_url, execution_plan,
			error_message, created_at, updated_at
		FROM jobs WHERE id = ?`

	var j model.Job
	err := r.db.QueryRowContext(ctx, q, id).Scan(
		&j.ID, &j.UserID, &j.Status, &j.OriginalPrompt, &j.RefinedPrompt,
		&j.OriginalImageURL, &j.GeneratedImageURL, &j.ExecutionPlan,
		&j.ErrorMessage, &j.CreatedAt, &j.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get job: %w", err)
	}
	return &j, nil
}

func (r *mysqlRepository) UpdateJobStatus(ctx context.Context, id, status string) error {
	const q = `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("orchestrator: update job status: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("orchestrator: rows affected: %w", err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *mysqlRepository) UpdateJob(ctx context.Context, job *model.Job) error {
	job.UpdatedAt = time.Now()

	const q = `
		UPDATE jobs
		SET status = ?, refined_prompt = ?, execution_plan = ?,
			generated_image_url = ?, error_message = ?, updated_at = ?
		WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q,
		job.Status, job.RefinedPrompt, job.ExecutionPlan,
		job.GeneratedImageURL, job.ErrorMessage, job.UpdatedAt, job.ID,
	)
	if err != nil {
		return fmt.Errorf("orchestrator: update job: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("orchestrator: rows affected: %w", err)
	}
	if n == 0 {
		return ErrJobNotFound
	}
	return nil
}

func (r *mysqlRepository) InsertStatusHistory(ctx context.Context, h *model.JobStatusHistory) error {
	h.CreatedAt = time.Now()

	var metaJSON json.RawMessage
	if h.Metadata != nil {
		metaJSON = h.Metadata
	} else {
		metaJSON = json.RawMessage("{}")
	}

	const q = `
		INSERT INTO job_status_history (id, job_id, from_status, to_status, service, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		h.ID, h.JobID, h.FromStatus, h.ToStatus, h.Service, metaJSON, h.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("orchestrator: insert status history: %w", err)
	}
	return nil
}
