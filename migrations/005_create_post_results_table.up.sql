CREATE TABLE post_results (
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
