package imagegenagent

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

const otelScopeRepository = "internal/workers/imagegenagent"

// ErrJobNotFound is returned when no job matches the given ID.
var ErrJobNotFound = errors.New("imagegenagent: job not found")

// Repository defines job row updates performed by the image-gen agent.
type Repository interface {
	UpdateAfterGeneration(ctx context.Context, jobID, generatedImageURL, status string) error
}

type mysqlRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by MySQL.
func NewRepository(db *sql.DB) Repository {
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) UpdateAfterGeneration(ctx context.Context, jobID, generatedImageURL, status string) error {
	ctx, span := otel.Tracer(otelScopeRepository).Start(ctx, "imagegenagent.Repository.UpdateAfterGeneration")
	defer span.End()

	span.SetAttributes(
		attribute.String("db.system", "mysql"),
		attribute.String("db.operation", "update"),
		attribute.String("job.id", jobID),
		attribute.String("job.status", status),
	)

	now := time.Now()
	const q = `UPDATE jobs SET generated_image_url = ?, status = ?, updated_at = ? WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q, generatedImageURL, status, now, jobID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("imagegenagent: update generated_image_url and status: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("imagegenagent: rows affected: %w", err)
	}
	if n == 0 {
		err := ErrJobNotFound
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
