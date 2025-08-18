#!/bin/bash

echo "📊 MMN Validator Staking Status Monitor"
echo "======================================"
echo "Real-time monitoring of validator staking information"
echo ""

cd "$(dirname "$0")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Function to check if network is running
check_network() {
    echo -e "${BLUE}🔍 Checking Network Status...${NC}"
    
    # Check for running MMN processes
    mmn_processes=$(ps aux | grep -E "bin/mmn|mmn " | grep -v grep | wc -l)
    echo "📊 MMN Processes Running: $mmn_processes"
    
    if [ $mmn_processes -eq 0 ]; then
        echo -e "${YELLOW}⚠️  No MMN network detected. Starting test network...${NC}"
        return 1
    fi
    
    # Check for bootstrap node
    bootstrap_running=$(ps aux | grep "mmn bootnode" | grep -v grep | wc -l)
    if [ $bootstrap_running -gt 0 ]; then
        echo "✅ Bootstrap Node: Running"
    fi
    
    # Check for validator nodes
    validator_running=$(ps aux | grep "mmn node" | grep -v grep | wc -l)
    echo "✅ Validator Nodes: $validator_running running"
    
    return 0
}

# Function to extract staking info from logs
extract_staking_info() {
    echo -e "\n${BLUE}📋 Extracting Staking Information...${NC}"
    
    # Look for validator logs
    if ls validator*.log >/dev/null 2>&1; then
        echo "📄 Found validator log files"
        
        # Extract PoS configurations
        echo -e "\n${YELLOW}⚖️  PoS Configuration:${NC}"
        pos_configs=$(grep -h "LeaderSchedule.*entries" validator*.log 2>/dev/null)
        if [ ! -z "$pos_configs" ]; then
            echo "$pos_configs" | head -3 | while IFS= read -r line; do
                echo "   $line"
            done
        else
            echo "   No PoS configuration found in logs"
        fi
        
        # Extract genesis information
        echo -e "\n${YELLOW}🌱 Genesis Configuration:${NC}"
        genesis_info=$(grep -h "Successfully loaded config" validator*.log 2>/dev/null)
        if [ ! -z "$genesis_info" ]; then
            echo "$genesis_info" | head -1 | sed 's/^.*config] /   /'
        fi
        
        # Extract faucet information
        faucet_info=$(grep -h "faucet.*balance" validator*.log 2>/dev/null)
        if [ ! -z "$faucet_info" ]; then
            echo -e "\n${YELLOW}💰 Faucet Information:${NC}"
            echo "$faucet_info" | head -1 | sed 's/^.*GENESIS.*: /   /'
        fi
        
    else
        echo "⚠️  No validator log files found"
        return 1
    fi
}

# Function to analyze genesis configuration
analyze_genesis_config() {
    echo -e "\n${BLUE}📊 Genesis Configuration Analysis:${NC}"
    
    genesis_file="config/genesis_with_staking.yml"
    if [ -f "$genesis_file" ]; then
        echo "✅ Found staking genesis config: $genesis_file"
        
        # Extract key staking parameters
        echo -e "\n${YELLOW}⚙️  Staking Parameters:${NC}"
        
        # Look for faucet configuration
        if grep -q "faucet" "$genesis_file"; then
            faucet_info=$(grep -A 2 "faucet:" "$genesis_file")
            echo "   💰 Faucet Configuration:"
            echo "$faucet_info" | sed 's/^/      /'
        fi
        
        # Look for leader schedule
        if grep -q "leader_schedule" "$genesis_file"; then
            echo "   📅 Leader Schedule: Configured"
            schedule_count=$(grep -c "validator_id" "$genesis_file" || echo "0")
            echo "   📊 Validators in Schedule: $schedule_count"
        fi
        
    else
        echo "⚠️  Staking genesis config not found: $genesis_file"
        
        # Check for basic genesis
        basic_genesis="config/genesis.yml"
        if [ -f "$basic_genesis" ]; then
            echo "📄 Found basic genesis config: $basic_genesis"
        fi
    fi
}

# Function to check validator keys
check_validator_keys() {
    echo -e "\n${BLUE}🔑 Validator Keys Status:${NC}"
    
    key_count=0
    for key_file in config/validator*.txt config/key*.txt; do
        if [ -f "$key_file" ]; then
            ((key_count++))
            key_size=$(stat -c%s "$key_file" 2>/dev/null || echo "0")
            echo "   ✅ $key_file (${key_size} bytes)"
        fi
    done
    
    if [ $key_count -eq 0 ]; then
        echo "   ⚠️  No validator keys found"
    else
        echo "   📊 Total validator keys: $key_count"
    fi
    
    # Check bootnode key
    if [ -f "config/bootnode_privkey.txt" ]; then
        echo "   ✅ Bootstrap key: config/bootnode_privkey.txt"
    else
        echo "   ⚠️  Bootstrap key not found"
    fi
}

# Function to monitor active validators
monitor_active_validators() {
    echo -e "\n${BLUE}👥 Active Validators Monitoring:${NC}"
    
    # Check gRPC ports
    echo -e "\n${YELLOW}🔗 gRPC Endpoint Status:${NC}"
    for port in 9101 9102 9103; do
        if netstat -tln 2>/dev/null | grep -q ":$port "; then
            echo "   ✅ Port $port: Active (Validator running)"
        else
            echo "   ❌ Port $port: Inactive"
        fi
    done
    
    # Check validator processes with memory usage
    echo -e "\n${YELLOW}💻 Validator Process Status:${NC}"
    validator_pids=$(ps aux | grep "mmn node" | grep -v grep | awk '{print $2}')
    if [ ! -z "$validator_pids" ]; then
        echo "   📊 Active Validator Processes:"
        ps -o pid,rss,vsz,pcpu,comm -p $validator_pids 2>/dev/null | while IFS= read -r line; do
            echo "      $line"
        done
    else
        echo "   ⚠️  No active validator processes found"
    fi
}

# Function to simulate staking query (if demo is available)
simulate_staking_query() {
    echo -e "\n${BLUE}🧪 Staking System Test:${NC}"
    
    if [ -f "client_test/staking/poh_pos_integration_demo.go" ]; then
        echo "📋 Running staking integration test..."
        echo ""
        
        # Run demo and capture specific staking output
        go run client_test/staking/poh_pos_integration_demo.go 2>/dev/null | grep -A 20 "Test 2: Staking System"
        
    elif [ -f "client_test/staking/staking_test_standalone.go" ]; then
        echo "📋 Running standalone staking test..."
        echo ""
        go run client_test/staking/staking_test_standalone.go 2>/dev/null | head -20
        
    else
        echo "⚠️  No staking test files available"
    fi
}

# Function to show stake distribution
show_stake_distribution() {
    echo -e "\n${BLUE}📊 Theoretical Stake Distribution:${NC}"
    echo "Based on MMN design (Equal Distribution):"
    echo ""
    echo "   👥 Validators: 10"
    echo "   🎯 Distribution: Equal (10% each)"
    echo "   💰 Stake per Validator: 10,000,000 tokens"
    echo "   💎 Total Network Stake: 100,000,000 tokens"
    echo "   🏆 Commission: 5% per validator"
    echo ""
    echo "   📈 Stake Distribution Table:"
    echo "   ┌─────────────┬────────────────┬─────────────┐"
    echo "   │ Validator   │ Stake Amount   │ Percentage  │"
    echo "   ├─────────────┼────────────────┼─────────────┤"
    for i in {1..10}; do
        printf "   │ Validator %-2d │ 10,000,000     │    10.0%%    │\n" $i
    done
    echo "   └─────────────┴────────────────┴─────────────┘"
}

# Main monitoring function
main_monitor() {
    echo -e "${BLUE}🚀 Starting Comprehensive Staking Monitor...${NC}"
    echo ""
    
    # Step 1: Check network
    if ! check_network; then
        echo -e "\n${YELLOW}📋 Network not running. Showing available information...${NC}"
    fi
    
    # Step 2: Extract from logs
    extract_staking_info
    
    # Step 3: Analyze genesis config
    analyze_genesis_config
    
    # Step 4: Check validator keys
    check_validator_keys
    
    # Step 5: Monitor active validators
    monitor_active_validators
    
    # Step 6: Show stake distribution
    show_stake_distribution
    
    # Step 7: Run staking test if available
    simulate_staking_query
    
    echo -e "\n${GREEN}📋 Staking Status Monitor Complete!${NC}"
    echo ""
    echo "💡 To start network and see live staking:"
    echo "   ./scripts/build_and_test.sh"
    echo ""
    echo "💡 To run detailed staking test:"
    echo "   go run client_test/staking/poh_pos_integration_demo.go"
}

# Run main monitor
main_monitor
