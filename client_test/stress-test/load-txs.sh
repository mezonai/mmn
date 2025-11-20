#!/bin/bash

# Load Test Background Runner
# Usage: ./load-txs.sh [parameters]

"$(dirname "$0")" >/dev/null 2>&1 && cd "$(dirname "$0")"

# Default parameters
ACCOUNTS=${1:-100}
RATE=${2:-40}
FUND=${3:-10000000000}
AMOUNT=${4:-100}
DURATION=${5:-0}
MINUTES=${6:-0}
TOTAL_TXS=${7:-0}

# Prepare logging
mkdir -p logs
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
LOG_FILE="logs/load_test_${TIMESTAMP}.log"

echo "Starting load test in background..."
echo "Parameters: accounts=$ACCOUNTS, rate=$RATE, fund=$FUND, amount=$AMOUNT, duration=${DURATION}s, minutes=$MINUTES, total_txs=$TOTAL_TXS"
echo "Log file: $LOG_FILE"

# Run in background with nohup
nohup go run . \
  -accounts $ACCOUNTS \
  -rate $RATE \
  -fund $FUND \
  -amount $AMOUNT \
  -duration ${DURATION}s \
  -minutes ${MINUTES} \
  -total_txs $TOTAL_TXS \
  > $LOG_FILE 2>&1 &

# Get the process ID
PID=$!
echo "Process started with PID: $PID"
echo "To stop: kill $PID"
echo "To view logs: tail -f $LOG_FILE"

# Save PID to file for easy management
echo $PID > load_test.pid
echo "PID saved to load_test.pid"