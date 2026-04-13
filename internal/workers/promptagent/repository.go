package promptagent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const otelScopeRepository = "internal/workers/promptagent"

// ErrJobNotFound is returned when no job matches the given ID.
var ErrJobNotFound = errors.New("promptagent: job not found")

// Repository defines job row updates performed by the prompt agent.
type Repository interface {
	UpdateRefinedPrompt(ctx context.Context, jobID, refinedPrompt string) error
}

type mysqlRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by MySQL.
func NewRepository(db *sql.DB) Repository {
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) UpdateRefinedPrompt(ctx context.Context, jobID, refinedPrompt string) error {
	ctx, span := otel.Tracer(otelScopeRepository).Start(ctx, "promptagent.Repository.UpdateRefinedPrompt")
	defer span.End()

	span.SetAttributes(
		attribute.String("db.system", "mysql"),
		attribute.String("db.operation", "update"),
		attribute.String("job.id", jobID),
	)

	now := time.Now()
	const q = `UPDATE jobs SET refined_prompt = ?, updated_at = ? WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q, refinedPrompt, now, jobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("promptagent: update refined_prompt: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("promptagent: rows affected: %w", err)
	}
	if n == 0 {
		err := ErrJobNotFound
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
