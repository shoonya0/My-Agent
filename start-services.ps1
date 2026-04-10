# MyAgent Services Startup Script (PowerShell)
# Runs api-gateway, auth-service, and approval-service in parallel

# Service definitions
$services = @(
    @{Name="api-gateway"; Path="cmd\api-gateway"},
    @{Name="auth-service"; Path="cmd\auth-service"},
    @{Name="approval-service"; Path="cmd\approval-service"}
)

# Array to hold job objects
$jobs = @()

# Function to cleanup on exit
function Cleanup {
    Write-Host ""
    Write-Host "Shutting down services..." -ForegroundColor Yellow
    foreach ($job in $jobs) {
        if ($job.State -eq "Running") {
            Write-Host "Stopping $($job.Name)..." -ForegroundColor Blue
            Stop-Job $job
            Remove-Job $job
        }
    }
    Write-Host "All services stopped." -ForegroundColor Green
    exit
}

# Register cleanup on Ctrl+C
$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action { Cleanup }

Write-Host "Starting MyAgent services..." -ForegroundColor Green
Write-Host ""

# Start each service
foreach ($service in $services) {
    Write-Host "Starting $($service.Name)..." -ForegroundColor Blue
    
    # Create logs directory if it doesn't exist
    $logsDir = Join-Path $service.Path "logs"
    if (!(Test-Path $logsDir)) {
        New-Item -ItemType Directory -Path $logsDir -Force | Out-Null
    }
    
    # Start service as background job with output redirected to log file
    $logFile = Join-Path $logsDir "app.log"
    $job = Start-Job -Name $service.Name -ScriptBlock {
        param($path, $logPath)
        Set-Location $path
        go run main.go *>&1 | Out-File -FilePath $logPath -Append
    } -ArgumentList (Join-Path $PSScriptRoot $service.Path), $logFile
    
    $jobs += $job
    Write-Host "[OK] $($service.Name) started (Job ID: $($job.Id))" -ForegroundColor Green
}

Write-Host ""
Write-Host "All services started successfully!" -ForegroundColor Green
Write-Host "Press Ctrl+C to stop all services" -ForegroundColor Yellow
Write-Host ""

# Display service endpoints
Write-Host "Service Endpoints:" -ForegroundColor Blue
Write-Host "  - API Gateway:      http://localhost:8080"
Write-Host "  - Auth Service:     gRPC on localhost:9090"
Write-Host "  - Approval Service: gRPC on localhost:9092"
Write-Host ""

# Display log file locations
Write-Host "Log files:" -ForegroundColor Cyan
Write-Host "  - cmd\api-gateway\logs\app.log"
Write-Host "  - cmd\auth-service\logs\app.log"
Write-Host "  - cmd\approval-service\logs\app.log"
Write-Host ""
Write-Host "To view logs in real-time, run:" -ForegroundColor Yellow
Write-Host "  Get-Content cmd\api-gateway\logs\app.log -Wait -Tail 20"
Write-Host ""

# Wait for Ctrl+C
try {
    while ($true) {
        # Check if any job has failed
        foreach ($job in $jobs) {
            if ($job.State -eq "Failed") {
                Write-Host "Service $($job.Name) failed! Check logs for details." -ForegroundColor Red
                Receive-Job $job -ErrorAction SilentlyContinue
            }
        }
        Start-Sleep -Seconds 2
    }
}
finally {
    Cleanup
}
