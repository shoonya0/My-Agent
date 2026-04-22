# Stop MyAgent Services
# Kills processes using application ports (not infra: Kafka uses 9092, Redis 6379, etc.)

$ports = @(8090, 9190, 9091, 9093)

Write-Host "Stopping MyAgent services..." -ForegroundColor Yellow

foreach ($port in $ports) {
    $connections = netstat -ano | findstr ":$port"
    
    if ($connections) {
        # Extract PIDs from netstat output
        $processIds = $connections | ForEach-Object {
            if ($_ -match '\s+(\d+)\s*$') {
                $matches[1]
            }
        } | Select-Object -Unique
        
        foreach ($processId in $processIds) {
            if ($processId -and $processId -ne "0") {
                try {
                    $process = Get-Process -Id $processId -ErrorAction SilentlyContinue
                    if ($process) {
                        Write-Host "Stopping process $($process.ProcessName) (PID: $processId) on port $port" -ForegroundColor Blue
                        Stop-Process -Id $processId -Force
                        Write-Host "[OK] Stopped PID $processId" -ForegroundColor Green
                    }
                }
                catch {
                    Write-Host "Could not stop PID $processId" -ForegroundColor Red
                }
            }
        }
    }
}

# Also stop any PowerShell background jobs
$jobs = Get-Job
if ($jobs) {
    Write-Host "Stopping background jobs..." -ForegroundColor Blue
    Stop-Job $jobs
    Remove-Job $jobs
    Write-Host "[OK] Background jobs stopped" -ForegroundColor Green
}

Write-Host ""
Write-Host "All services stopped." -ForegroundColor Green
