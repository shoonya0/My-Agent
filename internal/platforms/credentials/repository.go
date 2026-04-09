package credentials

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/dbutil"
	"myAgent/pkg/types"
)

var ErrCredentialNotFound = errors.New("credentials: not found")

// Repository defines the data-access contract for platform credentials.
type Repository interface {
	Upsert(ctx context.Context, cred *types.PlatformCredential) error
	GetByUserAndPlatform(ctx context.Context, userID, platform string) (*types.PlatformCredential, error)
	ListByUser(ctx context.Context, userID string) ([]types.PlatformCredential, error)
	Delete(ctx context.Context, userID, platform string) error
}

type mysqlRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by a MySQL database connection.
func NewRepository(db *sql.DB) Repository {
	return &mysqlRepository{db: db}
}

// Upsert inserts or updates a platform credential in the database.
func (r *mysqlRepository) Upsert(ctx context.Context, cred *types.PlatformCredential) error {
	now := time.Now()
	cred.UpdatedAt = now
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = now
	}

	const q = `
		INSERT INTO platform_credentials
			(id, user_id, platform, access_token_enc, refresh_token_enc,
			 token_expiry, scopes, platform_user_id, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			access_token_enc  = VALUES(access_token_enc),
			refresh_token_enc = VALUES(refresh_token_enc),
			token_expiry      = VALUES(token_expiry),
			scopes            = VALUES(scopes),
			platform_user_id  = VALUES(platform_user_id),
			metadata          = VALUES(metadata),
			updated_at        = VALUES(updated_at)`

	_, err := r.db.ExecContext(ctx, q,
		cred.ID, cred.UserID, cred.Platform,
		cred.AccessTokenEnc, cred.RefreshTokenEnc,
		cred.TokenExpiry, dbutil.JSONStringSlice(cred.Scopes),
		cred.PlatformUserID, dbutil.JSONMap(cred.Metadata),
		cred.CreatedAt, cred.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("credentials repo: upsert: %w", err)
	}
	return nil
}

func (r *mysqlRepository) GetByUserAndPlatform(ctx context.Context, userID, platform string) (*types.PlatformCredential, error) {
	const q = `
		SELECT id, user_id, platform, access_token_enc, refresh_token_enc,
		       token_expiry, scopes, platform_user_id, metadata, created_at, updated_at
		FROM platform_credentials
		WHERE user_id = ? AND platform = ?`

	var cred types.PlatformCredential
	var scopes dbutil.JSONStringSlice
	var metadata dbutil.JSONMap

	err := r.db.QueryRowContext(ctx, q, userID, platform).Scan(
		&cred.ID, &cred.UserID, &cred.Platform,
		&cred.AccessTokenEnc, &cred.RefreshTokenEnc,
		&cred.TokenExpiry, &scopes,
		&cred.PlatformUserID, &metadata,
		&cred.CreatedAt, &cred.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("credentials repo: get: %w", err)
	}
	cred.Scopes = scopes
	cred.Metadata = metadata
	return &cred, nil
}

func (r *mysqlRepository) ListByUser(ctx context.Context, userID string) ([]types.PlatformCredential, error) {
	const q = `
		SELECT id, user_id, platform, access_token_enc, refresh_token_enc,
		       token_expiry, scopes, platform_user_id, metadata, created_at, updated_at
		FROM platform_credentials
		WHERE user_id = ?
		ORDER BY platform`

	rows, err := r.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("credentials repo: list: %w", err)
	}
	defer rows.Close()

	var creds []types.PlatformCredential
	for rows.Next() {
		var c types.PlatformCredential
		var scopes dbutil.JSONStringSlice
		var metadata dbutil.JSONMap
		if err := rows.Scan(
			&c.ID, &c.UserID, &c.Platform,
			&c.AccessTokenEnc, &c.RefreshTokenEnc,
			&c.TokenExpiry, &scopes,
			&c.PlatformUserID, &metadata,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("credentials repo: scan: %w", err)
		}
		c.Scopes = scopes
		c.Metadata = metadata
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (r *mysqlRepository) Delete(ctx context.Context, userID, platform string) error {
	const q = `DELETE FROM platform_credentials WHERE user_id = ? AND platform = ?`

	res, err := r.db.ExecContext(ctx, q, userID, platform)
	if err != nil {
		return fmt.Errorf("credentials repo: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("credentials repo: rows affected: %w", err)
	}
	if n == 0 {
		return ErrCredentialNotFound
	}
	return nil
}
