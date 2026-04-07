# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/image-gen-agent ./cmd/image-gen-agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata procps \
    && adduser -D -u 65532 -g '' appuser
COPY --from=builder --chown=appuser:appuser /out/image-gen-agent /app/image-gen-agent
USER appuser
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD pgrep -x image-gen-agent || exit 1
ENTRYPOINT ["/app/image-gen-agent"]
