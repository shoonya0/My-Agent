#!/bin/bash

# MyAgent Services Startup Script
# Runs api-gateway, auth-service, and approval-service in parallel

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Service names and directories
SERVICES=(
  "api-gateway:cmd/api-gateway"
  "auth-service:cmd/auth-service"
  "approval-service:cmd/approval-service"
)

# Array to hold PIDs
declare -a PIDS

# Cleanup function to kill all services
cleanup() {
  echo -e "\n${YELLOW}Shutting down services...${NC}"
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      echo -e "${BLUE}Stopping process $pid${NC}"
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait
  echo -e "${GREEN}All services stopped.${NC}"
  exit 0
}

# Trap Ctrl+C and call cleanup
trap cleanup SIGINT SIGTERM

echo -e "${GREEN}Starting MyAgent services...${NC}\n"

# Start each service
for service_info in "${SERVICES[@]}"; do
  IFS=':' read -r service_name service_dir <<< "$service_info"
  
  echo -e "${BLUE}Starting ${service_name}...${NC}"
  
  # Create logs directory if it doesn't exist
  mkdir -p "$service_dir/logs"
  
  # Start service in background and redirect output to log file
  (
    cd "$service_dir"
    go run main.go >> "logs/app.log" 2>&1
  ) &
  
  # Store PID
  PIDS+=($!)
  echo -e "${GREEN}✓ ${service_name} started (PID: ${PIDS[-1]})${NC}"
done

echo -e "\n${GREEN}All services started successfully!${NC}"
echo -e "${YELLOW}Press Ctrl+C to stop all services${NC}\n"

# Display service endpoints
echo -e "${BLUE}Service Endpoints:${NC}"
echo -e "  • API Gateway:      http://localhost:8090"
echo -e "  • Auth Service:     gRPC on localhost:9190"
echo -e "  • Approval Service: gRPC on localhost:9093"
echo ""

# Wait for all background processes
wait
