#!/bin/bash

# Test script cho MMN WebSocket Events Service
# Script này sẽ start MMN node và test WebSocket functionality

set -e

echo "🚀 MMN WebSocket Events Service Test"
echo "===================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if build exists
if [ ! -f "bin/mmn" ]; then
    print_status "Building MMN..."
    go build -o bin/mmn ./cmd
    print_success "Build completed"
fi

# Create test transaction function
create_test_transaction() {
    local sender=$1
    local recipient=$2
    local amount=$3
    local nonce=$4
    
    cat << EOF
{
    "type": 0,
    "sender": "$sender",
    "recipient": "$recipient", 
    "amount": $amount,
    "timestamp": $(date +%s),
    "text_data": "test_payment",
    "nonce": $nonce
}
EOF
}

# Function to submit transaction
submit_transaction() {
    local api_port=$1
    local tx_data="$2"
    
    curl -s -X POST \
         -H "Content-Type: application/json" \
         -d "$tx_data" \
         "http://localhost:$api_port/txs" || true
}

# Function to check WebSocket health
check_websocket_health() {
    local ws_port=${1:-8090}
    local health_url="http://localhost:$ws_port/health"
    
    local response=$(curl -s "$health_url" 2>/dev/null || echo "")
    if [ -n "$response" ]; then
        echo "$response" | jq . 2>/dev/null || echo "$response"
        return 0
    else
        return 1
    fi
}

# Function to test WebSocket with curl
test_websocket_with_curl() {
    local ws_port=${1:-8090}
    print_status "Testing WebSocket connection with curl..."
    
    # Use websocat if available, otherwise skip
    if command -v websocat >/dev/null 2>&1; then
        timeout 5s websocat "ws://localhost:$ws_port/ws" &
        local ws_pid=$!
        sleep 2
        kill $ws_pid 2>/dev/null || true
        print_success "WebSocket connection test completed"
    else
        print_warning "websocat not found, skipping WebSocket connection test"
        print_status "Install websocat with: cargo install websocat"
    fi
}

# Cleanup function
cleanup() {
    print_status "Cleaning up..."
    
    # Kill MMN node
    if [ -n "$MMN_PID" ]; then
        kill $MMN_PID 2>/dev/null || true
        print_status "Stopped MMN node (PID: $MMN_PID)"
    fi
    
    # Kill Python client if running
    if [ -n "$PYTHON_PID" ]; then
        kill $PYTHON_PID 2>/dev/null || true
        print_status "Stopped Python client (PID: $PYTHON_PID)"
    fi
    
    exit 0
}

# Set trap for cleanup
trap cleanup EXIT INT TERM

# Main test flow
main() {
    print_status "Starting MMN node..."
    
    # Start MMN node in background
    ./bin/mmn -node=node1 > mmn.log 2>&1 &
    MMN_PID=$!
    
    print_status "MMN node started (PID: $MMN_PID)"
    print_status "Waiting for services to start..."
    
    # Wait for services to start
    sleep 5
    
    # Check if node is running
    if ! kill -0 $MMN_PID 2>/dev/null; then
        print_error "MMN node failed to start"
        cat mmn.log
        exit 1
    fi
    
    print_success "MMN node is running"
    
    # Check WebSocket health
    print_status "Checking WebSocket health..."
    local retries=5
    local ws_healthy=false
    
    for i in $(seq 1 $retries); do
        if check_websocket_health 8090; then
            ws_healthy=true
            break
        fi
        print_status "Waiting for WebSocket service... ($i/$retries)"
        sleep 2
    done
    
    if [ "$ws_healthy" = true ]; then
        print_success "WebSocket service is healthy"
    else
        print_error "WebSocket service health check failed"
        exit 1
    fi
    
    # Test WebSocket connection
    test_websocket_with_curl 8090
    
    # Start Python client if available
    if command -v python3 >/dev/null 2>&1; then
        if python3 -c "import websockets" 2>/dev/null; then
            print_status "Starting Python WebSocket client..."
            timeout 30s python3 websocket/client.py --no-reconnect > python_client.log 2>&1 &
            PYTHON_PID=$!
            print_status "Python client started (PID: $PYTHON_PID)"
            sleep 2
        else
            print_warning "Python websockets library not found"
            print_status "Install with: pip install websockets"
        fi
    fi
    
    # Generate some test transactions
    print_status "Generating test transactions..."
    
    # Sample keys for testing
    local sender_key="1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
    local recipient_key="fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321"
    
    # Submit multiple transactions
    for i in {1..5}; do
        local tx_data=$(create_test_transaction "$sender_key" "$recipient_key" $((1000 * i)) $i)
        print_status "Submitting transaction $i..."
        
        local response=$(submit_transaction 8001 "$tx_data")
        if [ "$response" = "ok" ]; then
            print_success "Transaction $i submitted successfully"
        else
            print_warning "Transaction $i submission response: $response"
        fi
        
        sleep 1
    done
    
    # Wait for events to be processed
    print_status "Waiting for events to be processed..."
    sleep 10
    
    # Check final WebSocket health
    print_status "Final WebSocket health check..."
    check_websocket_health 8090
    
    # Display logs
    print_status "Displaying recent logs..."
    echo ""
    echo "=== MMN Node Logs (last 20 lines) ==="
    tail -n 20 mmn.log || true
    echo ""
    
    if [ -f "python_client.log" ]; then
        echo "=== Python Client Logs ==="
        cat python_client.log || true
        echo ""
    fi
    
    # Instructions for manual testing
    print_success "Test completed!"
    echo ""
    print_status "Manual testing options:"
    echo "1. Open websocket/client.html in your browser"
    echo "2. Connect to ws://localhost:8090/ws"
    echo "3. Submit transactions via API: curl -X POST -d '{...}' http://localhost:8001/txs"
    echo "4. Check WebSocket health: curl http://localhost:8090/health"
    echo ""
    print_status "Press Ctrl+C to stop all services"
    
    # Keep running until interrupted
    wait $MMN_PID
}

# Check dependencies
print_status "Checking dependencies..."

if ! command -v go >/dev/null 2>&1; then
    print_error "Go is not installed"
    exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
    print_error "curl is not installed"
    exit 1
fi

if command -v jq >/dev/null 2>&1; then
    print_success "jq found - JSON output will be formatted"
else
    print_warning "jq not found - JSON output will be raw"
fi

# Run main test
main
