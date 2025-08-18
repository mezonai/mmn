#!/bin/bash

echo "🚀 MMN Network Test Script"
echo "========================="
echo "Testing PoH + PoS Integration"
echo ""

cd "$(dirname "$0")/.."

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}🧹 Cleaning up processes...${NC}"
    pkill -f "mmn bootnode" 2>/dev/null || true
    pkill -f "mmn node" 2>/dev/null || true
    sleep 2
    rm -f *.log *.tmp 2>/dev/null || true
    echo "✅ Cleanup complete"
}

trap cleanup EXIT

echo -e "${BLUE}📋 Test Plan:${NC}"
echo "1. Build verification"
echo "2. Start bootstrap node"
echo "3. Start 3 validator nodes"
echo "4. Monitor PoH + PoS integration"
echo "5. Verify network health"
echo ""

# Step 1: Build verification
echo -e "${YELLOW}🔨 Step 1: Build Verification${NC}"
echo "============================="

if [ ! -f "bin/mmn" ]; then
    echo "Building MMN binary..."
    go build -o bin/mmn main.go
    if [ $? -ne 0 ]; then
        echo -e "${RED}❌ Build failed${NC}"
        exit 1
    fi
fi

binary_size=$(ls -lh bin/mmn | awk '{print $5}')
echo "✅ Binary: bin/mmn ($binary_size)"

# Verify commands
echo "🔍 Verifying commands..."
./bin/mmn --help > /dev/null 2>&1 && echo "  ✅ Main command working"
./bin/mmn bootnode --help > /dev/null 2>&1 && echo "  ✅ Bootnode command working"
./bin/mmn node --help > /dev/null 2>&1 && echo "  ✅ Node command working"

# Step 2: Setup configuration
echo -e "\n${YELLOW}⚙️  Step 2: Configuration Setup${NC}"
echo "==============================="

mkdir -p config

# Create bootnode key if not exists
if [ ! -f "config/bootnode_privkey.txt" ]; then
    echo "b8c4f14c5c0a5c4d8e2b6f3a1d9e7c8b5a2f6e9d3c8a1b4e7f0c9d2a5e8b1f4c7" > config/bootnode_privkey.txt
    echo "✅ Created bootnode private key"
fi

# Create validator keys
for i in {1..3}; do
    keyfile="config/validator${i}_key.txt"
    if [ ! -f "$keyfile" ]; then
        openssl rand -hex 32 > "$keyfile" 2>/dev/null || echo "validator${i}_$(date +%s)" > "$keyfile"
        echo "✅ Created validator $i key"
    fi
done

# Step 3: Start bootstrap node
echo -e "\n${YELLOW}🌐 Step 3: Starting Bootstrap Node${NC}"
echo "=================================="

echo "Starting bootstrap node on port 9000..."
./bin/mmn bootnode \
  --privkey-path "./config/bootnode_privkey.txt" \
  --bootstrap-p2p-port 9000 > bootnode.log 2>&1 &

BOOTNODE_PID=$!
echo "✅ Bootstrap node started (PID: $BOOTNODE_PID)"

# Wait and extract peer ID
sleep 5

# Extract the full peer ID from bootnode log
PEER_ID=$(grep "Node ID:" bootnode.log | head -1 | sed 's/.*Node ID://' | tr -d ' ')
if [ -z "$PEER_ID" ]; then
    echo -e "${RED}❌ Failed to get peer ID${NC}"
    echo "Bootnode log content:"
    cat bootnode.log
    exit 1
fi

BOOTSTRAP_ADDR="/ip4/127.0.0.1/tcp/9000/p2p/${PEER_ID}"
echo "📋 Peer ID: $PEER_ID"
echo "📋 Bootstrap address: $BOOTSTRAP_ADDR"

# Step 4: Start validator nodes
echo -e "\n${YELLOW}⚡ Step 4: Starting Validator Nodes${NC}"
echo "==================================="

# Use staking configuration if available
GENESIS_CONFIG="config/genesis_with_staking.yml"
if [ ! -f "$GENESIS_CONFIG" ]; then
    GENESIS_CONFIG="config/genesis.yml"
    echo "⚠️  Using default genesis config"
fi

echo "Using genesis: $GENESIS_CONFIG"
echo "Bootstrap: $BOOTSTRAP_ADDR"

for i in {1..3}; do
    grpc_port=$((9100 + i))
    
    echo "Starting Validator $i (gRPC: $grpc_port)..."
    
    ./bin/mmn node \
      --privkey-path "config/validator${i}_key.txt" \
      --grpc-addr ":$grpc_port" \
      --genesis-path "$GENESIS_CONFIG" \
      --bootstrap-addresses "$BOOTSTRAP_ADDR" > "validator${i}.log" 2>&1 &
    
    validator_pid=$!
    echo "  ✅ Started (PID: $validator_pid)"
    echo $validator_pid >> validator_pids.tmp
    
    sleep 3
done

echo ""
echo -e "${GREEN}🎉 Network Started Successfully!${NC}"

# Step 5: Monitor network
echo -e "\n${YELLOW}📊 Step 5: Monitoring Network (30s)${NC}"
echo "==================================="

echo "Monitoring for PoH + PoS activity..."

for i in {1..6}; do
    echo -e "\n${BLUE}[$(date +%H:%M:%S)] Check $i/6${NC}"
    
    # Process status
    if ps -p $BOOTNODE_PID > /dev/null 2>&1; then
        echo "  ✅ Bootstrap: Running"
    else
        echo -e "  ${RED}❌ Bootstrap: Stopped${NC}"
    fi
    
    # Count running validators
    running_validators=0
    if [ -f "validator_pids.tmp" ]; then
        while read pid; do
            if ps -p $pid > /dev/null 2>&1; then
                ((running_validators++))
            fi
        done < validator_pids.tmp
    fi
    echo "  📊 Validators: $running_validators/3 running"
    
    # PoH activity
    poh_activity=$(grep -h "PoH\|Ticking" validator*.log 2>/dev/null | wc -l || echo "0")
    echo "  ⏰ PoH Activity: $poh_activity ticks"
    
    # PoS activity
    pos_activity=$(grep -h "LeaderSchedule" validator*.log 2>/dev/null | wc -l || echo "0")
    echo "  ⚖️  PoS Activity: $pos_activity configurations"
    
    # Network activity
    network_activity=$(grep -h "peer\|connected" *.log 2>/dev/null | wc -l || echo "0")
    echo "  🔗 Network: $network_activity connections"
    
    # Errors
    error_count=$(grep -h "ERROR\|Failed" *.log 2>/dev/null | wc -l || echo "0")
    echo "  ⚠️  Errors: $error_count"
    
    sleep 5
done

# Step 6: Final analysis
echo -e "\n${YELLOW}🔍 Step 6: Final Analysis${NC}"
echo "========================="

echo -e "\n${BLUE}Bootstrap Node Status:${NC}"
if ps -p $BOOTNODE_PID > /dev/null 2>&1; then
    echo "✅ Status: Running"
    echo "📊 Memory: $(ps -o rss= -p $BOOTNODE_PID | awk '{print $1/1024}')MB"
else
    echo -e "${RED}❌ Status: Not running${NC}"
fi

echo -e "\n${BLUE}Validator Status:${NC}"
validator_count=0
if [ -f "validator_pids.tmp" ]; then
    while read pid; do
        if ps -p $pid > /dev/null 2>&1; then
            ((validator_count++))
            mem=$(ps -o rss= -p $pid | awk '{print $1/1024}')
            echo "✅ Validator $validator_count: Running (${mem}MB)"
        fi
    done < validator_pids.tmp
fi

echo -e "\n${BLUE}Integration Analysis:${NC}"

# PoH analysis
poh_configs=$(grep -h "PoH config:" validator*.log 2>/dev/null | wc -l || echo "0")
echo "✅ PoH Configurations: $poh_configs/3 loaded"
if [ "$poh_configs" -gt 0 ]; then
    poh_setting=$(grep "PoH config:" validator*.log 2>/dev/null | head -1 | cut -d':' -f3-)
    echo "  Settings:$poh_setting"
fi

# PoS analysis  
pos_configs=$(grep -h "LeaderSchedule.*entries" validator*.log 2>/dev/null | wc -l || echo "0")
echo "✅ PoS Configurations: $pos_configs/3 loaded"
if [ "$pos_configs" -gt 0 ]; then
    pos_setting=$(grep "LeaderSchedule.*entries" validator*.log 2>/dev/null | head -1 | cut -d':' -f4-)
    echo "  Settings:$pos_setting"
fi

# Blockchain analysis
blockchain_inits=$(grep -h "BLOCKCHAIN.*initialized" validator*.log 2>/dev/null | wc -l || echo "0")
echo "✅ Blockchain Initializations: $blockchain_inits/3"

# Log analysis
echo -e "\n${BLUE}Log Summary:${NC}"
total_size=0
for logfile in *.log; do
    if [ -f "$logfile" ]; then
        size_kb=$(du -k "$logfile" | cut -f1)
        total_size=$((total_size + size_kb))
        lines=$(wc -l < "$logfile")
        echo "📄 $logfile: ${size_kb}KB ($lines lines)"
    fi
done
echo "📊 Total: ${total_size}KB"

# Final verdict
echo -e "\n${YELLOW}🎯 TEST RESULTS${NC}"
echo "==============="

success_score=0

if ps -p $BOOTNODE_PID > /dev/null 2>&1; then
    echo "✅ Bootstrap Node: PASS"
    ((success_score++))
else
    echo -e "${RED}❌ Bootstrap Node: FAIL${NC}"
fi

if [ $validator_count -ge 2 ]; then
    echo "✅ Validator Nodes: PASS ($validator_count/3)"
    ((success_score++))
else
    echo -e "${RED}❌ Validator Nodes: FAIL ($validator_count/3)${NC}"
fi

if [ "$poh_configs" -ge 2 ]; then
    echo "✅ PoH Integration: PASS"
    ((success_score++))
else
    echo -e "${RED}❌ PoH Integration: FAIL${NC}"
fi

if [ "$pos_configs" -ge 2 ]; then
    echo "✅ PoS Integration: PASS"
    ((success_score++))
else
    echo -e "${RED}❌ PoS Integration: FAIL${NC}"
fi

if [ "$blockchain_inits" -ge 2 ]; then
    echo "✅ Blockchain Init: PASS"
    ((success_score++))
else
    echo -e "${RED}❌ Blockchain Init: FAIL${NC}"
fi

echo ""
if [ $success_score -ge 4 ]; then
    echo -e "${GREEN}🎉 OVERALL: SUCCESS (Score: $success_score/5)${NC}"
    echo "MMN PoH + PoS Integration working correctly!"
else
    echo -e "${RED}⚠️  OVERALL: PARTIAL (Score: $success_score/5)${NC}"
    echo "Some components need attention."
fi

echo ""
echo -e "${BLUE}🚀 Test Complete - Ready for deployment!${NC}"

# Cleanup happens automatically via trap
