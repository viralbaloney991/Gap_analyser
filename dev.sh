#!/usr/bin/env bash
set -euo pipefail

BACKEND_DIR="$(cd "$(dirname "$0")/backend" && pwd)"
FRONTEND_DIR="$(cd "$(dirname "$0")/frontend" && pwd)"
BACKEND_LOG=/tmp/backend.log
FRONTEND_LOG=/tmp/frontend.log
PID_FILE=/tmp/dev-servers.pid

stop() {
  if [[ -f "$PID_FILE" ]]; then
    while IFS= read -r pid; do
      kill "$pid" 2>/dev/null && echo "  killed $pid" || true
    done < "$PID_FILE"
    rm -f "$PID_FILE"
  fi
  # Belt-and-suspenders: kill by pattern too
  pkill -f "go run ./cmd/server" 2>/dev/null || true
  pkill -f "vite"                2>/dev/null || true
  # Force-free ports in case any process is still bound
  lsof -ti :8080 2>/dev/null | xargs kill -9 2>/dev/null || true
  lsof -ti :5173 2>/dev/null | xargs kill -9 2>/dev/null || true
  echo "Servers stopped."
}

start() {
  stop 2>/dev/null || true  # clean up any stale processes first

  echo "Starting backend..."
  (cd "$BACKEND_DIR" && go run ./cmd/server > "$BACKEND_LOG" 2>&1) &
  BACKEND_PID=$!

  echo "Starting frontend..."
  (cd "$FRONTEND_DIR" && npm run dev > "$FRONTEND_LOG" 2>&1) &
  FRONTEND_PID=$!

  printf "%s\n%s\n" "$BACKEND_PID" "$FRONTEND_PID" > "$PID_FILE"

  echo "Waiting for servers to initialise..."
  local attempts=0
  until curl -sf http://localhost:8080/api/health > /dev/null 2>&1; do
    sleep 1
    (( attempts++ ))
    if (( attempts >= 30 )); then
      echo "Backend failed to start. Last logs:"
      tail -20 "$BACKEND_LOG"
      stop
      exit 1
    fi
  done

  echo ""
  echo "  Backend  → http://localhost:8080  (logs: $BACKEND_LOG)"
  echo "  Frontend → http://localhost:5173  (logs: $FRONTEND_LOG)"
  echo ""
  tail -6 "$BACKEND_LOG"
}

case "${1:-start}" in
  start)  start ;;
  stop)   stop  ;;
  restart) stop; sleep 1; start ;;
  logs)
    echo "=== BACKEND ===" && tail -30 "$BACKEND_LOG"
    echo "=== FRONTEND ===" && tail -10 "$FRONTEND_LOG"
    ;;
  *)
    echo "Usage: $0 [start|stop|restart|logs]"
    exit 1
    ;;
esac
