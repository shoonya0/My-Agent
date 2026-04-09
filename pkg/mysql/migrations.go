package mysql

import (
	"context"
	"database/sql"
	"fmt"
)

// AutoMigrate creates all required tables if they don't exist.
// Tables are created in dependency order: users -> jobs -> job_status_history, platform_credentials, post_results
func AutoMigrate(ctx context.Context, db *sql.DB) error {
	migrations := []struct {
		name  string
		query string
	}{
		{
			name: "users",
			query: `
				CREATE TABLE IF NOT EXISTS users (
					id VARCHAR(36) PRIMARY KEY,
					email VARCHAR(255) NOT NULL,
					password_hash VARCHAR(255) NULL,
					display_name VARCHAR(255) NOT NULL,
					avatar_url VARCHAR(512) NULL,
					provider VARCHAR(50) NOT NULL DEFAULT 'local',
					provider_id VARCHAR(255) NULL,
					roles JSON NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
					UNIQUE KEY uk_users_email (email),
					INDEX idx_provider_id (provider, provider_id)
				) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
			`,
		},
		{
			name: "jobs",
			query: `
				CREATE TABLE IF NOT EXISTS jobs (
					id VARCHAR(36) PRIMARY KEY,
					user_id VARCHAR(36) NOT NULL,
					status VARCHAR(50) NOT NULL,
					original_prompt TEXT NOT NULL,
					refined_prompt TEXT NULL,
					original_image_url VARCHAR(512) NOT NULL,
					generated_image_url VARCHAR(512) NULL,
					execution_plan JSON NULL,
					error_message TEXT NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
					INDEX idx_user_id (user_id),
					INDEX idx_status (status),
					INDEX idx_created_at (created_at),
					CONSTRAINT fk_jobs_user FOREIGN KEY (user_id) REFERENCES users (id)
				) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
			`,
		},
		{
			name: "job_status_history",
			query: `
				CREATE TABLE IF NOT EXISTS job_status_history (
					id VARCHAR(36) PRIMARY KEY,
					job_id VARCHAR(36) NOT NULL,
					from_status VARCHAR(50) NULL,
					to_status VARCHAR(50) NOT NULL,
					service VARCHAR(50) NOT NULL,
					metadata JSON NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					INDEX idx_job_id (job_id),
					INDEX idx_created_at (created_at),
					CONSTRAINT fk_job_status_history_job FOREIGN KEY (job_id) REFERENCES jobs (id)
				) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
			`,
		},
		{
			name: "platform_credentials",
			query: `
				CREATE TABLE IF NOT EXISTS platform_credentials (
					id VARCHAR(36) PRIMARY KEY,
					user_id VARCHAR(36) NOT NULL,
					platform VARCHAR(50) NOT NULL,
					access_token_enc TEXT NOT NULL,
					refresh_token_enc TEXT NULL,
					token_expiry TIMESTAMP NULL,
					scopes JSON NULL,
					platform_user_id VARCHAR(255) NULL,
					metadata JSON NULL,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
					UNIQUE KEY unique_user_platform (user_id, platform),
					INDEX idx_user_id (user_id),
					CONSTRAINT fk_platform_credentials_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
				) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
			`,
		},
		{
			name: "post_results",
			query: `
				CREATE TABLE IF NOT EXISTS post_results (
					id VARCHAR(36) PRIMARY KEY,
					job_id VARCHAR(36) NOT NULL,
					user_id VARCHAR(36) NOT NULL,
					platform VARCHAR(50) NOT NULL,
					status VARCHAR(50) NOT NULL,
					platform_post_id VARCHAR(255) NULL,
					platform_url VARCHAR(512) NULL,
					error_detail TEXT NULL,
					attempt_count INT NOT NULL DEFAULT 1,
					created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
					INDEX idx_job_id (job_id),
					INDEX idx_user_id (user_id),
					INDEX idx_status (status),
					CONSTRAINT fk_post_results_job FOREIGN KEY (job_id) REFERENCES jobs (id),
					CONSTRAINT fk_post_results_user FOREIGN KEY (user_id) REFERENCES users (id)
				) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
			`,
		},
	}

	for _, migration := range migrations {
		if _, err := db.ExecContext(ctx, migration.query); err != nil {
			return fmt.Errorf("failed to create table %s: %w", migration.name, err)
		}
	}

	return nil
}
