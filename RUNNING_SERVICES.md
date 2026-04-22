# Running MyAgent Services

This document explains how to run all MyAgent services together using the provided startup scripts.

## Available Startup Scripts

Three scripts are provided for different environments:

### 1. **start-services.sh** (Bash - Git Bash, WSL, Linux, macOS)

**Usage:**

```bash
# Make executable (first time only)
chmod +x start-services.sh

# Run
./start-services.sh
```

**Features:**

- Runs all services in the background
- Single terminal window
- Logs written to `cmd/{service}/logs/app.log`
- Graceful shutdown with Ctrl+C
- Shows all service PIDs

### 2. **start-services.ps1** (PowerShell)

**Usage:**

```powershell
# Run
.\start-services.ps1
```

**Features:**

- Runs services as PowerShell background jobs
- Logs written to `cmd/{service}/logs/app.log`
- Graceful shutdown with Ctrl+C
- Single PowerShell window
- Shows log file locations and tail command for real-time viewing

### 3. **start-services.bat** (Windows CMD Recommended)

**Usage:**

```cmd
start-services.bat
```

**Features:**

- Opens each service in a separate terminal window
- Easy to see each service's logs separately
- Close individual windows to stop specific services

## Service Endpoints

When all services are running:

| Service              | Type    | Endpoint              |
| -------------------- | ------- | --------------------- |
| **API Gateway**      | HTTP/WS | http://localhost:8090 |
| **Auth Service**     | gRPC    | localhost:9190        |
| **Approval Service** | gRPC    | localhost:9093        |

## Stopping Services

### Using stop-services.ps1 (Recommended) (PowerShell)

```powershell
.\stop-services.ps1
```

This will automatically find and kill all service processes on ports 8090, 9190, 9091, 9093 (not Kafka on 9092).

### Manual methods

#### Bash script

Press `Ctrl+C` in the terminal - all services will stop gracefully

#### PowerShell script

Press `Ctrl+C` in the PowerShell window - all jobs will be stopped

#### Batch script

Close individual terminal windows or press `Ctrl+C` in each window

## Viewing Logs

Logs are written to:

- `cmd/api-gateway/logs/app.log`
- `cmd/auth-service/logs/app.log`
- `cmd/approval-service/logs/app.log`

You can tail logs in another terminal:

```bash
# Bash/WSL/Git Bash
tail -f cmd/api-gateway/logs/app.log

# PowerShell
Get-Content cmd\api-gateway\logs\app.log -Wait
```

## Prerequisites

Before running any script, ensure:

1. **Go is installed** (`go version` should work)
2. **Dependencies are installed** (run `go mod download` in the project root)
3. **Configuration is set up** (environment variables or config.env file)
4. **MySQL is running** on the configured port (default: 3307)
5. **Redis is running** on the configured port (default: 6379)

## Troubleshooting

**Port already in use:**

- Check if services are already running: `lsof -i :8090` (Unix) or `netstat -ano | findstr :8090` (Windows)
- Kill existing processes or change ports in configuration

**Services not starting:**

- Check individual log files in `cmd/{service}/logs/app.log`
- Verify environment variables are set correctly
- Ensure database connections are working

**Permission denied (Bash):**

```bash
chmod +x start-services.sh
```

## Recommended Workflow

For development, choose based on your preference:

- **Use bash script** if you want a clean single-terminal experience with logs in files
- **Use PowerShell script** if you want to see live aggregated logs
- **Use batch script** if you want to see each service's logs in separate windows

For production, use a process manager like systemd, supervisord, or Docker Compose.
