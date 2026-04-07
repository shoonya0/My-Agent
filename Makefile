# MyAgent — database migrations (requires Go, MYSQL_DSN or DATABASE_URL in env).
# On Windows, use Git Bash / WSL, or run the go run lines from PowerShell.

MIGRATE_PATH ?= migrations

.PHONY: migrate-up migrate-down migrate-version migrate-force migrate-create

migrate-up:
	go run ./cmd/migrate -cmd up -path $(MIGRATE_PATH)

migrate-down:
	go run ./cmd/migrate -cmd down -path $(MIGRATE_PATH)

migrate-version:
	go run ./cmd/migrate -cmd version -path $(MIGRATE_PATH)

# Repair schema_migrations after a failed migration (use with care). Example: make migrate-force VERSION=4
migrate-force:
	@test -n "$(VERSION)" || (echo "usage: make migrate-force VERSION=N" && exit 1)
	go run ./cmd/migrate -cmd force -path $(MIGRATE_PATH) -version $(VERSION)

# Example: make migrate-create NAME=add_indexes
migrate-create:
	@test -n "$(NAME)" || (echo "usage: make migrate-create NAME=description" && exit 1)
	go run ./cmd/migrate -cmd create -path $(MIGRATE_PATH) -name $(NAME)
