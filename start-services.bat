@echo off
REM MyAgent Services Startup Script (Windows Batch)
REM Runs api-gateway, auth-service, and approval-service in parallel

echo Starting MyAgent services...
echo.

REM Create logs directories
if not exist "cmd\api-gateway\logs" mkdir "cmd\api-gateway\logs"
if not exist "cmd\auth-service\logs" mkdir "cmd\auth-service\logs"
if not exist "cmd\approval-service\logs" mkdir "cmd\approval-service\logs"

REM Start each service in a new window
echo Starting api-gateway...
start "API Gateway" cmd /c "cd cmd\api-gateway && go run main.go"

echo Starting auth-service...
start "Auth Service" cmd /c "cd cmd\auth-service && go run main.go"

echo Starting approval-service...
start "Approval Service" cmd /c "cd cmd\approval-service && go run main.go"

echo.
echo All services started in separate windows!
echo.
echo Service Endpoints:
echo   - API Gateway:      http://localhost:8090
echo   - Auth Service:     gRPC on localhost:9190
echo   - Approval Service: gRPC on localhost:9093
echo.
echo Close the individual terminal windows to stop each service.
echo.
pause
