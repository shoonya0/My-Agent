# Port allocation and conflict resolution

This document summarizes the port map for My-Agent, changes made to avoid common conflicts, and files that were updated.

## Original vs new assignments

| Role | Original (problematic) | New | Reason |
|------|------------------------|-----|--------|
| API Gateway (HTTP/WS) | 8080 | **8090** | 8080 is used by many dev servers and proxies. |
| Auth service (gRPC) | 9090 | **9190** | 9090 is commonly used by monitoring tools; changed to avoid potential conflicts. |
| Auth service (HTTP) | 8082 | 8082 | Unchanged; not a common conflict. |
| Orchestrator (gRPC) | 9091 | 9091 | Unchanged. |
| Approval (gRPC) | 9093 | 9093 | Unchanged. |
| Test stack Kafka (host) | 9093 | **9094** | **Duplicate:** host port 9093 was used for both test Kafka and approval gRPC when running tests + apps locally. |

## Infrastructure ports (unchanged in `docker/docker-compose.yml`)

These use usual defaults and were not changed:

| Service | Host port(s) |
|---------|----------------|
| Redis | 6379 |
| Zookeeper | 2181 |
| Kafka | 9092 |
| MySQL | 3307 |
| Jaeger UI / OTLP / collector | 16686 / 4317 / 14268 |
| MinIO S3 / console | 9000 / 9001 |

## Test stack (`docker-compose.test.yml`)

| Service | Host port |
|---------|-----------|
| Zookeeper | 2182 |
| Kafka | **9094** (was 9093) |
| MySQL | 3307 |
| Redis | 6380 |
| Jaeger | 16687 / 4318 |

## Files modified

- `pkg/types/config.go` — defaults: `API_GATEWAY_PORT` 8090, `GRPC_PORT` / `AUTH_SERVICE_ADDR` 9190.
- `pkg/infrastructure/httpserver/server.go` — comment example port.
- `config.env`, `config.env.example` — ports and header comments.
- `example.env` — `PORT`, `GRPC_PORT`.
- `docker/docker-compose.yml` — comment block documenting app/monitoring ports.
- `docker-compose.test.yml` — Kafka host **9094**, fixed usage comment (3307/6380/9094).
- `deploy/docker/api-gateway/Dockerfile` — `EXPOSE` / healthcheck **8090**.
- `deploy/docker/auth-service/Dockerfile` — `EXPOSE` **9190** 8082.
- `stop-services.ps1` — stop app ports **8090, 9190, 9091, 9093** (removed **9092** so Kafka is not killed by mistake).
- `start-services.ps1`, `start-services.sh`, `start-services.bat` — printed URLs; approval corrected to **9093** (was wrongly **9092**).
- `README.md`, `RUNNING_SERVICES.md`, `docs/REFRESH_TOKEN_GUIDE.md` — documentation and examples.

## Recommendations

1. Inside Compose, keep `AUTH_SERVICE_ADDR=auth-service:9190` only if the auth container listens on 9190; if you prefer internal **9090** in-container, use host mapping `9190:9090` and keep `AUTH_SERVICE_ADDR=auth-service:9090` for in-network clients while `config.env` uses `localhost:9190` for host-based dev.
2. Prefer `stop-services.ps1` only when infra (Kafka, etc.) should keep running; the script no longer targets port 9092.
