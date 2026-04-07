# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/approval-service ./cmd/approval-service

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -u 65532 -g '' appuser
COPY --from=builder --chown=appuser:appuser /out/approval-service /app/approval-service
USER appuser
EXPOSE 8086
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8086/health || exit 1
ENTRYPOINT ["/app/approval-service"]
