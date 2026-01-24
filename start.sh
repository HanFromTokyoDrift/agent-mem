#!/bin/bash
set -e

cd "$(dirname "$0")"

echo "=========================================="
echo "  Project Cortex - Starting Services"
echo "=========================================="

# 1. Start Database
echo "[1/4] Starting Database..."
docker-compose up -d

# 2. Wait for DB
echo "[2/4] Waiting for DB to be ready..."
sleep 3

# 3. Start API Server (Background)
echo "[3/4] Starting API Server..."
export PYTHONPATH="$(pwd)"
export PYTHONUNBUFFERED=1
nohup .venv/bin/python src/server.py > server.log 2>&1 &
API_PID=$!
echo "      API Server PID: $API_PID"

# 4. Start Watcher (Background)
echo "[4/4] Starting File Watcher..."
nohup .venv/bin/python src/watcher.py > watcher.log 2>&1 &
WATCHER_PID=$!
echo "      Watcher PID: $WATCHER_PID"

echo ""
echo "=========================================="
echo "  Project Cortex is running!"
echo "=========================================="
echo ""
echo "  API Docs:  http://localhost:8000/docs"
echo "  Query:     curl -X POST http://localhost:8000/query -H 'Content-Type: application/json' -d '{"\""query"\"": "\""test"\""}'"
echo ""
echo "  View logs: tail -f server.log watcher.log"
echo "  Stop:      pkill -f 'src/server.py' && pkill -f 'src/watcher.py'"
echo ""
