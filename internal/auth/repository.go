package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"myAgent/pkg/dbutil"
	"myAgent/pkg/types"
)

// ErrUserNotFound is returned when no user matches the query.
var ErrUserNotFound = errors.New("auth: user not found")

// Repository defines the data-access contract for user records.
type Repository interface {
	CreateUser(ctx context.Context, user *types.User) error
	GetUserByEmail(ctx context.Context, email string) (*types.User, error)
	GetUserByProviderID(ctx context.Context, provider, providerID string) (*types.User, error)
	UpdateUser(ctx context.Context, user *types.User) error
}

type mysqlRepository struct {
	db *sql.DB
}

// NewRepository returns a Repository backed by a MySQL database connection.
func NewRepository(db *sql.DB) Repository {
	return &mysqlRepository{db: db}
}

func (r *mysqlRepository) CreateUser(ctx context.Context, user *types.User) error {
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now

	const q = `
		INSERT INTO users (id, email, password_hash, display_name, avatar_url, provider, provider_id, roles, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := r.db.ExecContext(ctx, q,
		user.ID, user.Email, user.PasswordHash, user.DisplayName,
		user.AvatarURL, user.Provider, user.ProviderID,
		dbutil.JSONStringSlice(user.Roles), user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("auth: create user: %w", err)
	}
	return nil
}

func (r *mysqlRepository) GetUserByEmail(ctx context.Context, email string) (*types.User, error) {
	const q = `SELECT id, email, password_hash, display_name, avatar_url, provider, provider_id, roles, created_at, updated_at
		FROM users WHERE email = ?`
	return r.scanUser(ctx, q, email)
}

func (r *mysqlRepository) GetUserByProviderID(ctx context.Context, provider, providerID string) (*types.User, error) {
	const q = `SELECT id, email, password_hash, display_name, avatar_url, provider, provider_id, roles, created_at, updated_at
		FROM users WHERE provider = ? AND provider_id = ?`
	return r.scanUser(ctx, q, provider, providerID)
}

func (r *mysqlRepository) UpdateUser(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()

	const q = `
		UPDATE users
		SET email = ?, display_name = ?, avatar_url = ?, roles = ?, updated_at = ?
		WHERE id = ?`

	res, err := r.db.ExecContext(ctx, q,
		user.Email, user.DisplayName, user.AvatarURL,
		dbutil.JSONStringSlice(user.Roles), user.UpdatedAt, user.ID,
	)
	if err != nil {
		return fmt.Errorf("auth: update user: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("auth: rows affected: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *mysqlRepository) scanUser(ctx context.Context, query string, args ...any) (*types.User, error) {
	var u types.User
	var roles dbutil.JSONStringSlice

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.AvatarURL,
		&u.Provider, &u.ProviderID, &roles, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: scan user: %w", err)
	}
	u.Roles = roles
	return &u, nil
}
