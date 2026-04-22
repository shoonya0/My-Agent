# MyAgent

> **AI-powered image editing and social distribution pipeline**

MyAgent is a microservices-based platform that transforms user-provided images through natural language prompts, generates AI-edited versions via ComfyUI, and distributes approved results to multiple social media platforms.

[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)
[![Architecture](https://img.shields.io/badge/Architecture-Microservices-blue)](docs/architecture.md)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## Table of Contents

- [Overview](#overview)
- [Key Features](#key-features)
- [Architecture](#architecture)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [Running Services](#running-services)
- [API Documentation](#api-documentation)
- [Project Structure](#project-structure)
- [Development](#development)
- [Observability](#observability)
- [Testing](#testing)
- [Documentation](#documentation)
- [Contributing](#contributing)

---

## Overview

MyAgent processes images through a sophisticated pipeline:

1. **Submission**: User uploads image + natural language prompt
2. **Orchestration**: LLM parses intent and creates execution plan
3. **Refinement**: AI agent optimizes prompt for ComfyUI
4. **Generation**: ComfyUI generates edited image
5. **Approval**: User previews result via WebSocket
6. **Distribution**: Approved images post to selected social platforms

### Flow Diagram

```
User ──POST /api/v1/jobs──▸ API Gateway ──gRPC──▸ Orchestrator (LLM intent parsing)
  │
  └──Kafka: prompt.refine.requested──▸ Prompt Agent (LLM refinement)
       │
       └──Kafka: prompt.refined──▸ Image Gen Agent (ComfyUI)
            │
            └──Kafka: image.generated──▸ Approval Service ──gRPC Stream──▸ API Gateway
                 │
                 └──WebSocket /ws/{job_id}──▸ User (preview)
                      │
                      └──POST /api/v1/jobs/{id}/approve──▸ Orchestrator
                           │
                           └──Kafka: image.approved──▸ Distribution Service
                                │
                                └──▸ Instagram / Discord / Telegram / WhatsApp
```

---

## Key Features

### 🤖 AI-Powered Processing
- **LLM Intent Parsing**: Orchestrator uses GPT-4o to understand user prompts
- **Prompt Refinement**: Specialized agent optimizes prompts for image generation
- **ComfyUI Integration**: Professional-grade image editing workflows

### 🔐 Secure Authentication
- **JWT-based auth** with refresh tokens
- **OAuth2 support** (Google, GitHub, Facebook)
- **Token blacklisting** via Redis
- **Per-user encrypted credentials** for social platforms (AES-256-GCM)

### 🌐 Multi-Platform Distribution
- **Instagram** (Graph API)
- **Discord** (Webhooks)
- **Telegram** (Bot API)
- **WhatsApp** (Business Cloud API)
- Parallel fan-out with individual error handling

### ⚡ Real-Time Updates
- **WebSocket** connections for job status notifications
- **gRPC streaming** between approval-service and api-gateway
- Live preview delivery with presigned S3 URLs

### 🏗️ Modern Architecture
- **API Gateway pattern** (single public HTTP entry)
- **Event-driven** (Kafka for async workflows)
- **gRPC for internal RPCs** (auth, orchestrator, approval)
- **Clean Architecture** (handler → service → repository layers)

### 📊 Production-Ready Observability
- **OpenTelemetry** distributed tracing (Jaeger)
- **Structured logging** (Zap, JSON format)

---

## Architecture

### Service Overview

| Service | Role | Protocol | Public |
|---------|------|----------|--------|
| **api-gateway** | Single public entry: REST, WebSocket, OAuth callbacks | HTTP/WS + gRPC client | ✅ Yes |
| **auth-service** | JWT issuance, OAuth processing, token validation | gRPC | ❌ Internal |
| **orchestrator** | Job lifecycle, LLM intent parsing, Kafka events | gRPC + Kafka | ❌ Internal |
| **prompt-agent** | LLM-based prompt refinement | Kafka | ❌ Internal |
| **image-gen-agent** | ComfyUI workflow execution | Kafka | ❌ Internal |
| **approval-service** | Job preview caching, gRPC streaming to gateway | gRPC + Kafka | ❌ Internal |
| **distribution-service** | Multi-platform posting with encrypted credentials | Kafka | ❌ Internal |

### Port Allocation

| Service/Component | Port | Type | Notes |
|-------------------|------|------|-------|
| **MySQL** | 3307 | TCP | Database |
| **Redis** | 6379 | TCP | Cache & blacklist |
| **Kafka** | 9092 | TCP | Event bus |
| **api-gateway** | 8090 | HTTP/WS | Public REST + WebSocket |
| **auth-service** | 9190 | gRPC | Internal auth RPCs |
| **orchestrator** | 9091 | gRPC | Internal job RPCs |
| **approval-service** | 9093 | gRPC | Streaming job updates |
| **ComfyUI** | 8188 | HTTP | External (image generation) |
| **Jaeger** | 4317 | gRPC | OTLP collector |
| **Jaeger UI** | 16686 | HTTP | Trace visualization |

### Technology Stack

- **Language**: Go 1.25
- **Web Framework**: Gin
- **Database**: MySQL (go-sql-driver/mysql)
- **Cache**: Redis (go-redis/v9)
- **Message Queue**: Kafka (Sarama)
- **RPC**: gRPC with Protocol Buffers
- **LLM**: OpenAI GPT-4o
- **Image Processing**: ComfyUI
- **Storage**: AWS S3 (SDK v2) or MinIO
- **Observability**: OpenTelemetry, Zap, Jaeger
- **Authentication**: JWT (golang-jwt/v5), OAuth2

---

## Prerequisites

### Required
- **Go** 1.25+ ([install](https://go.dev/dl/))
- **MySQL** 8.0+ (for user & job persistence)
- **Redis** 6.0+ (for JWT blacklist, rate limiting, caching)
- **Kafka** 3.0+ (for event-driven workflows)
- **ComfyUI** running on port 8188 ([setup guide](https://github.com/comfyanonymous/ComfyUI))
- **OpenAI API Key** (for LLM orchestration & refinement)

### Optional
- **Docker** & **Docker Compose** (for running infrastructure)
- **Jaeger** (for distributed tracing)
- **AWS S3** or **MinIO** (for image storage)

---

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/yourusername/MyAgent.git
cd MyAgent
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Configure Environment

```bash
# Copy example config
cp config.env.example config.env

# Edit config.env with your settings
# REQUIRED: MYSQL_DSN, REDIS_ADDR, JWT_SECRET, OPENAI_API_KEY, ENCRYPTION_KEY
```

### 4. Initialize Database

```bash
# Create database
mysql -u root -p -e "CREATE DATABASE myagent;"

# Run migrations (auto-run on first service startup)
# Or manually execute: internal/migrations/schema.sql
```

### 5. Start Infrastructure (Docker Compose)

```bash
docker-compose up -d mysql redis kafka jaeger
```

### 6. Start Services

#### Option A: All services (Bash/Git Bash/WSL)
```bash
chmod +x start-services.sh
./start-services.sh
```

#### Option B: All services (PowerShell)
```powershell
.\start-services.ps1
```

#### Option C: Individual services
```bash
# Terminal 1: API Gateway
go run cmd/api-gateway/main.go

# Terminal 2: Auth Service
go run cmd/auth-service/main.go

# Terminal 3: Orchestrator
go run cmd/orchestrator/main.go

# Terminal 4: Prompt Agent
go run cmd/prompt-agent/main.go

# Terminal 5: Image Gen Agent
go run cmd/image-gen-agent/main.go

# Terminal 6: Approval Service
go run cmd/approval-service/main.go

# Terminal 7: Distribution Service
go run cmd/distribution/main.go
```

### 7. Verify Services

```bash
# Health check
curl http://localhost:8090/health

# Expected response:
# {"status":"ok"}
```

---

## Configuration

Configuration is managed via **environment variables** or a `config.env` file. All services load the same config via `internal/config.Load()`.

### Core Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MYSQL_DSN` | ✅ | — | MySQL connection string |
| `REDIS_ADDR` | ✅ | — | Redis host:port |
| `JWT_SECRET` | ✅ | — | Secret for JWT signing (min 32 chars) |
| `KAFKA_BROKERS` | ✅ | — | Comma-separated Kafka brokers |
| `OPENAI_API_KEY` | ✅ | — | OpenAI API key |
| `COMFYUI_BASE_URL` | ✅ | — | ComfyUI endpoint (e.g., `http://localhost:8188`) |
| `ENCRYPTION_KEY` | ✅ | — | 64-char hex key for AES-256-GCM credential encryption |
| `AWS_BUCKET` | ✅ | — | S3 bucket name for image storage |

### Service Ports

| Variable | Default | Description |
|----------|---------|-------------|
| `API_GATEWAY_PORT` | `8090` | Public HTTP/WebSocket port |
| `GRPC_PORT` | `9190` | Auth service gRPC port |
| `ORCHESTRATOR_GRPC_PORT` | `9091` | Orchestrator gRPC port |
| `APPROVAL_GRPC_PORT` | `9093` | Approval service gRPC streaming port |

### Optional: OAuth2

| Variable | Provider | Description |
|----------|----------|-------------|
| `GOOGLE_OAUTH_CLIENT_ID` | Google | OAuth2 client ID |
| `GOOGLE_OAUTH_CLIENT_SECRET` | Google | OAuth2 client secret |
| `GOOGLE_OAUTH_REDIRECT_URL` | Google | Redirect URL (e.g., `http://localhost:8090/auth/google/callback`) |
| `GITHUB_OAUTH_CLIENT_ID` | GitHub | OAuth2 client ID |
| `GITHUB_OAUTH_CLIENT_SECRET` | GitHub | OAuth2 client secret |
| `GITHUB_OAUTH_REDIRECT_URL` | GitHub | Redirect URL |

### Optional: Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Logging level (debug, info, warn, error) |
| `JAEGER_ENDPOINT` | `localhost:4317` | OTLP gRPC endpoint for traces |

For complete configuration options, see [`config.env.example`](config.env.example).

---

## Running Services

### Using Startup Scripts (Recommended)

Three scripts are provided for different environments:

#### Bash (Git Bash, WSL, Linux, macOS)
```bash
chmod +x start-services.sh
./start-services.sh
# Press Ctrl+C to stop all services
```

#### PowerShell
```powershell
.\start-services.ps1
# Press Ctrl+C to stop all services
```

#### Windows Batch (separate windows)
```cmd
start-services.bat
# Close individual windows to stop services
```

### Stopping Services

#### Automated (PowerShell)
```powershell
.\stop-services.ps1
```

#### Manual
```bash
# Find processes
lsof -i :8090,:9190,:9091,:9093  # Unix
netstat -ano | findstr :8090     # Windows

# Kill by PID
kill <PID>        # Unix
taskkill /PID <PID> /F  # Windows
```

### Viewing Logs

Logs are written to `cmd/{service}/logs/app.log`:

```bash
# Bash/WSL
tail -f cmd/api-gateway/logs/app.log

# PowerShell
Get-Content cmd\api-gateway\logs\app.log -Wait
```

For more details, see [RUNNING_SERVICES.md](RUNNING_SERVICES.md).

---

## API Documentation

### Base URL
```
http://localhost:8090
```

### Authentication

All protected endpoints require a JWT token in the `Authorization` header:

```
Authorization: Bearer <access_token>
```

### Endpoints

#### 🔓 Public Endpoints

**POST /api/register**
Register a new user.

```bash
curl -X POST http://localhost:8090/api/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "secure123",
    "display_name": "John Doe"
  }'
```

**Response:**
```json
{
  "success": true,
  "message": "User registered successfully",
  "data": {
    "access_token": "eyJhbGc...",
    "refresh_token": "eyJhbGc...",
    "expires_in": 3600,
    "token_type": "Bearer"
  }
}
```

**POST /api/login**
Authenticate existing user.

```bash
curl -X POST http://localhost:8090/api/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "password": "secure123"
  }'
```

**GET /auth/{provider}/callback**
OAuth2 callback (Google, GitHub).

```
http://localhost:8090/auth/google/callback?code=xyz&state=abc
```

#### 🔒 Protected Endpoints

**POST /api/v1/jobs**
Submit a new image editing job.

```bash
curl -X POST http://localhost:8090/api/v1/jobs \
  -H "Authorization: Bearer <token>" \
  -F "image=@photo.jpg" \
  -F "prompt=Make the sky dramatic with sunset colors" \
  -F "platforms=instagram,discord" \
  -F "caption=Beautiful sunset edit!"
```

**Response:**
```json
{
  "success": true,
  "message": "Job submitted successfully",
  "data": {
    "job_id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "pending",
    "ws_url": "ws://localhost:8090/ws/550e8400-e29b-41d4-a716-446655440000",
    "created_at": "2026-04-12T10:30:00Z"
  }
}
```

**WebSocket /ws/{job_id}**
Real-time job status updates.

```javascript
const ws = new WebSocket('ws://localhost:8090/ws/550e8400-e29b-41d4-a716-446655440000');

ws.onmessage = (event) => {
  const notification = JSON.parse(event.data);
  // {
  //   "type": "preview_ready",
  //   "job_id": "550e8400-...",
  //   "status": "awaiting_approval",
  //   "preview_url": "https://s3.amazonaws.com/..."
  // }
};
```

**POST /api/v1/jobs/{job_id}/approve**
Approve generated image for distribution.

```bash
curl -X POST http://localhost:8090/api/v1/jobs/{job_id}/approve \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "caption": "Updated caption",
    "platforms": ["instagram", "telegram"]
  }'
```

**POST /api/v1/jobs/{job_id}/reject**
Reject generated image.

```bash
curl -X POST http://localhost:8090/api/v1/jobs/{job_id}/reject \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "Colors too saturated"
  }'
```

**GET /api/v1/jobs/{job_id}**
Get job details and results.

```bash
curl -X GET http://localhost:8090/api/v1/jobs/{job_id} \
  -H "Authorization: Bearer <token>"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "distributing",
    "original_prompt": "Make the sky dramatic",
    "refined_prompt": "cinematic sunset sky, dramatic clouds...",
    "original_image_url": "https://s3.../original.jpg",
    "generated_image_url": "https://s3.../generated.jpg",
    "post_results": [
      {
        "platform": "instagram",
        "status": "success",
        "platform_post_id": "18012345678901234",
        "platform_url": "https://instagram.com/p/..."
      }
    ],
    "created_at": "2026-04-12T10:30:00Z"
  }
}
```

#### 🔐 Platform Credentials

**POST /api/v1/credentials**
Connect social platform account.

```bash
curl -X POST http://localhost:8090/api/v1/credentials \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "platform": "telegram",
    "token": "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
    "metadata": {
      "chat_id": "-1001234567890"
    }
  }'
```

**GET /api/v1/credentials**
List connected platforms.

**DELETE /api/v1/credentials/{platform}**
Disconnect platform.

For complete API documentation, see [API Reference](docs/API.md) (TODO).

---

## Project Structure

```
MyAgent/
├── cmd/                        # Service entry points
│   ├── api-gateway/           # Public HTTP/WebSocket gateway
│   ├── auth-service/          # gRPC auth service
│   ├── orchestrator/          # Job orchestration
│   ├── prompt-agent/          # LLM prompt refinement
│   ├── image-gen-agent/       # ComfyUI integration
│   ├── approval-service/      # Job preview & gRPC streaming
│   └── distribution/          # Multi-platform posting
├── internal/                   # Private application code
│   ├── apigateway/            # Gateway handlers & routing
│   ├── auth/                  # Auth business logic
│   ├── jobs/                  # Job orchestration & approval
│   ├── platforms/             # Platform credential management
│   │   └── credentials/       # Encrypted credential storage
│   ├── workers/               # Kafka consumers
│   └── config/                # Configuration loading
├── pkg/                        # Shared libraries
│   ├── connectors/            # Platform-specific posting logic
│   ├── crypto/                # AES-256-GCM encryption
│   ├── data/                  # Database, Redis, Kafka clients
│   ├── infrastructure/        # Server, bootstrap, observability
│   ├── logger/                # Structured logging (Zap)
│   ├── middleware/            # JWT auth, rate limiting
│   ├── types/                 # Domain models & contracts
│   └── websocket/             # WebSocket hub
├── api/                        # gRPC proto definitions
│   ├── proto/                 # .proto files
│   ├── authpb/                # Generated auth service
│   ├── orchestratorpb/        # Generated orchestrator service
│   └── approvalpb/            # Generated approval service
├── docs/                       # Additional documentation
├── scripts/                    # Utility scripts
├── config.env.example         # Configuration template
├── docker-compose.yml         # Infrastructure setup
├── go.mod                     # Go module dependencies
└── README.md                  # This file
```

### Key Directories

- **`cmd/`**: Each subdirectory is a deployable service binary
- **`internal/`**: Application-specific code (not importable externally)
- **`pkg/`**: Reusable packages (could be extracted to separate repos)
- **`api/`**: gRPC service definitions and generated code

---

## Development

### Prerequisites
- Go 1.25+
- golangci-lint ([install](https://golangci-lint.run/usage/install/))
- protoc & protoc-gen-go ([install](https://grpc.io/docs/languages/go/quickstart/))

### Code Style

This project follows **Clean Architecture** principles:

```
Handler (HTTP/gRPC/Kafka)
    ↓
Service (Business Logic)
    ↓
Repository (Data Access)
```

**Key conventions:**
- **Explicit error handling** with wrapped errors (`fmt.Errorf("context: %w", err)`)
- **Interface-driven design** (inject dependencies via constructors)
- **Context propagation** for request-scoped values & cancellation
- **Table-driven tests** with parallel execution
- **GoDoc comments** on all exported functions

### Running Tests

```bash
# All tests
go test ./...

# With coverage
go test -cover ./...

# Specific package
go test ./pkg/connectors/...

# Verbose
go test -v ./internal/auth/...

# Race detector
go test -race ./...
```

### Linting

```bash
# Run all linters
golangci-lint run

# Auto-fix issues
golangci-lint run --fix

# Format code
go fmt ./...
goimports -w .
```

### Generating gRPC Code

After modifying `.proto` files:

```bash
# Install tools (first time)
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate
protoc --go_out=. --go-grpc_out=. api/proto/*.proto
```

### Adding a New Service

1. Create `cmd/{service}/main.go` entry point
2. Implement business logic in `internal/{service}/`
3. Define interfaces in `pkg/types/contracts.go` (if shared)
4. Add configuration to `pkg/types/config.go`
5. Update `docker-compose.yml` if needed
6. Document in this README

### Adding a Platform Connector

1. Implement `pkg/types.PlatformConnector` interface
2. Create `pkg/connectors/{platform}.go`
3. Register in `pkg/connectors/registry.go`
4. Update `distribution-service` to use connector
5. Document credential requirements

---

## Observability

### Distributed Tracing

All services export traces to **Jaeger** via OpenTelemetry:

```bash
# View traces
open http://localhost:16686
```

**Trace propagation:**
- HTTP: `otelgin.Middleware()` on api-gateway
- gRPC: `otelgrpc.NewClientHandler()` / `NewServerHandler()`
- Kafka: Manual `TraceCtx map[string]string` in event payloads

### Structured Logging

All services use **Zap** with JSON output:

```json
{
  "ts": "2026-04-12T10:30:00.123Z",
  "level": "info",
  "msg": "Job submitted",
  "service": "orchestrator",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "user_id": "user123"
}
```

### Observability

The project uses OpenTelemetry for distributed tracing through Jaeger:

```bash
# Access Jaeger UI
open http://localhost:16686
```

All services instrument key operations (HTTP requests, gRPC calls, database queries, Kafka messages) with spans for detailed request tracing.

---

## Testing

### Test Structure

```
internal/{package}/
├── handler.go
├── handler_test.go
├── service.go
├── service_test.go
├── repository.go
└── repository_test.go
```

### Test Categories

**Unit Tests**: Fast, no external dependencies
```bash
go test -short ./...
```

**Integration Tests**: Require MySQL, Redis, Kafka
```bash
go test -tags=integration ./...
```

### Example Test

```go
func TestSubmitJob(t *testing.T) {
    tests := []struct {
        name    string
        input   *SubmitJobRequest
        wantErr bool
    }{
        {
            name: "valid job",
            input: &SubmitJobRequest{
                Prompt:    "edit sky",
                Platforms: []string{"instagram"},
            },
            wantErr: false,
        },
        {
            name: "missing prompt",
            input: &SubmitJobRequest{
                Platforms: []string{"instagram"},
            },
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            err := service.SubmitJob(context.Background(), tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

---

## Documentation

- **[Architecture Reference](.cursor/rules/myagent-architecture.mdc)**: Detailed system design
- **[Running Services](RUNNING_SERVICES.md)**: Service startup guide
- **[Refresh Token Guide](docs/REFRESH_TOKEN_GUIDE.md)**: JWT refresh implementation
- **[TODO](Info/TODO.md)**: Development roadmap
- **[API Reference](docs/API.md)**: Complete endpoint documentation (TODO)

---

## Contributing

We welcome contributions! Please follow these guidelines:

1. **Fork** the repository
2. **Create a feature branch** (`git checkout -b feature/amazing-feature`)
3. **Follow Go conventions** and Clean Architecture patterns
4. **Write tests** for new functionality
5. **Run linters** (`golangci-lint run`)
6. **Update documentation** (README, architecture doc, GoDoc comments)
7. **Commit** with clear messages (`git commit -m 'Add Instagram Stories support'`)
8. **Push** to your fork (`git push origin feature/amazing-feature`)
9. **Open a Pull Request**

### Code Review Checklist

- [ ] Tests pass (`go test ./...`)
- [ ] Linters pass (`golangci-lint run`)
- [ ] Documentation updated
- [ ] No secrets in commits
- [ ] Error handling explicit
- [ ] Observability spans added
- [ ] Breaking changes documented

---

## Security

### Reporting Vulnerabilities

Please report security issues to **security@example.com** (do not create public issues).

### Security Features

- **JWT with short expiry** (1 hour) + refresh tokens
- **Token blacklisting** (logout via Redis)
- **Encrypted platform credentials** (AES-256-GCM at rest)
- **Rate limiting** (Redis-based)
- **Input validation** on all endpoints
- **OAuth2 CSRF protection** (state parameter)

---

## License

This project is licensed under the **MIT License**. See [LICENSE](LICENSE) for details.

---

## Acknowledgments

- [ComfyUI](https://github.com/comfyanonymous/ComfyUI) for image generation
- [OpenAI](https://openai.com/) for GPT-4o LLM capabilities
- [Gin](https://gin-gonic.com/) for HTTP routing
- [gRPC](https://grpc.io/) for efficient RPC
- [OpenTelemetry](https://opentelemetry.io/) for observability
- [Jaeger](https://www.jaegertracing.io/) for distributed tracing

---

## Support

- **Issues**: [GitHub Issues](https://github.com/yourusername/MyAgent/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/MyAgent/discussions)
- **Documentation**: [Wiki](https://github.com/yourusername/MyAgent/wiki)

---

**Built with ❤️ using Go and modern microservices architecture**
