#!/bin/bash

# Check Load Test Status
# Usage: ./status.sh

if [ -f "load_test.pid" ]; then
    PID=$(cat load_test.pid)
    if ps -p $PID > /dev/null 2>&1; then
        echo "✅ Load test is running (PID: $PID)"
        echo "📊 Process info:"
        ps -p $PID -o pid,ppid,cmd,etime,pcpu,pmem
        echo ""
        echo "📝 Recent logs:"
        if [ -d "logs" ]; then
            LATEST_LOG=$(ls -t logs/load_test_*.log 2>/dev/null | head -1)
            if [ -n "$LATEST_LOG" ]; then
                echo "--- Last 10 lines from $LATEST_LOG ---"
                tail -10 "$LATEST_LOG"
            fi
        fi
    else
        echo "❌ Load test process not found (PID: $PID)"
        echo "Cleaning up stale PID file..."
        rm -f load_test.pid
    fi
else
    echo "❌ No load test process found"
fi