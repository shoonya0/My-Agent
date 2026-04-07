# Development image: bind-mount the repo and use `go run` (see docker-compose.dev.yml).
# Not used for production images.
# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*
ENV CGO_ENABLED=0
