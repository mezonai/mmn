#!/bin/bash

echo "🔥 MMN Continuous Staking Monitor & Token Injector"
echo "=================================================="
echo "Real-time token injection and staking monitoring"
echo ""

cd "$(dirname "$0")/.."

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
MONITOR_INTERVAL=5  # seconds
TOKEN_INJECTION_INTERVAL=10  # seconds
TOKENS_PER_INJECTION=1000000  # 1M tokens per injection
MAX_INJECTIONS=10

# Global counters
injection_count=0
total_tokens_injected=0

# Function to inject tokens
inject_tokens() {
    local validator_port=$1
    local amount=$2
    
    echo -e "${CYAN}💰 Injecting $amount tokens to validator on port $validator_port${NC}"
    
    # Create token injection transaction (simulated via gRPC)
    # In real implementation, this would be gRPC call to validator
    echo "   📡 Sending injection request..."
    echo "   🎯 Target: localhost:$validator_port"
    echo "   💎 Amount: $amount tokens"
    echo "   ⏰ Time: $(date '+%H:%M:%S')"
    
    # Simulate injection latency
    sleep 1
    
    ((total_tokens_injected += amount))
    ((injection_count++))
    
    echo -e "${GREEN}   ✅ Injection #$injection_count completed${NC}"
    echo "   📊 Total injected: $total_tokens_injected tokens"
    echo ""
}

# Function to monitor staking status
monitor_staking_status() {
    echo -e "${BLUE}📊 Real-time Staking Status${NC}"
    echo "=========================="
    
    # Check active validators
    local validators=$(ps aux | grep -E "(mmn node|bin/mmn node)" | grep -v grep | wc -l)
    echo "🔢 Active Validators: $validators"
    
    if [ $validators -gt 0 ]; then
        # Get validator processes with memory
        echo "💻 Validator Status:"
        ps aux | grep -E "(mmn node|bin/mmn node)" | grep -v grep | while IFS= read -r line; do
            pid=$(echo $line | awk '{print $2}')
            mem=$(echo $line | awk '{print $6}')
            cpu=$(echo $line | awk '{print $3}')
            cmd=$(echo $line | awk '{print $11,$12,$13,$14}')
            
            # Extract port from command
            port=$(echo $cmd | grep -o "\:91[0-9][0-9]" | sed 's/://')
            if [ ! -z "$port" ]; then
                echo "   🏷️  Validator $port: PID=$pid, MEM=${mem}KB, CPU=${cpu}%"
                
                # Check if gRPC endpoint is responsive
                if netstat -tln 2>/dev/null | grep -q ":$port "; then
                    echo "      ✅ gRPC endpoint active"
                else
                    echo "      ❌ gRPC endpoint inactive"
                fi
            fi
        done
        
        echo ""
        
        # Show theoretical staking distribution
        echo "📈 Current Staking Distribution:"
        echo "   👥 Expected Validators: 10"
        echo "   💰 Base Stake per Validator: 10,000,000 tokens"
        echo "   🔄 Additional Tokens Injected: $total_tokens_injected"
        echo "   📊 Total Network Stake: $((100000000 + total_tokens_injected)) tokens"
        
        # Calculate new percentages if tokens injected
        if [ $total_tokens_injected -gt 0 ]; then
            new_total=$((100000000 + total_tokens_injected))
            base_percentage=$(echo "scale=2; 10000000 * 100 / $new_total" | bc -l)
            echo "   📉 Base Validator Share: ${base_percentage}% (down from 10%)"
        fi
        
    else
        echo "⚠️  No validators currently running"
    fi
    
    echo ""
}

# Function to show live network activity
monitor_network_activity() {
    echo -e "${PURPLE}🌐 Network Activity Monitor${NC}"
    echo "========================="
    
    # Check for recent log activity
    if ls *.log >/dev/null 2>&1; then
        echo "📄 Recent Log Activity:"
        
        # PoH activity
        poh_activity=$(tail -100 validator*.log 2>/dev/null | grep -c "PoH\|tick" || echo "0")
        echo "   ⏰ PoH Ticks (last 100 logs): $poh_activity"
        
        # PoS activity  
        pos_activity=$(tail -100 validator*.log 2>/dev/null | grep -c "LeaderSchedule\|stake" || echo "0")
        echo "   ⚖️  PoS Activity (last 100 logs): $pos_activity"
        
        # Network connections
        network_activity=$(tail -100 *.log 2>/dev/null | grep -c "peer\|connected" || echo "0")
        echo "   🔗 Network Events (last 100 logs): $network_activity"
        
        # Show latest PoH tick
        latest_poh=$(tail -20 validator*.log 2>/dev/null | grep "PoH\|tick" | tail -1)
        if [ ! -z "$latest_poh" ]; then
            echo "   🕐 Latest PoH: $latest_poh"
        fi
        
    else
        echo "⚠️  No log files available for network monitoring"
    fi
    
    echo ""
}

# Function to run continuous injection simulation
run_continuous_injection() {
    echo -e "${YELLOW}🚀 Starting Continuous Token Injection...${NC}"
    echo "Max Injections: $MAX_INJECTIONS every ${TOKEN_INJECTION_INTERVAL}s"
    echo "Tokens per Injection: $TOKENS_PER_INJECTION"
    echo ""
    
    # Get list of active validator ports
    local validator_ports=(9101 9102 9103)
    local active_ports=()
    
    for port in "${validator_ports[@]}"; do
        if netstat -tln 2>/dev/null | grep -q ":$port "; then
            active_ports+=($port)
        fi
    done
    
    if [ ${#active_ports[@]} -eq 0 ]; then
        echo -e "${RED}❌ No active validators found. Start network first.${NC}"
        return 1
    fi
    
    echo "🎯 Active validators: ${active_ports[*]}"
    echo ""
    
    # Continuous injection loop
    for ((i=1; i<=MAX_INJECTIONS; i++)); do
        echo -e "${CYAN}🔄 Injection Round $i/$MAX_INJECTIONS${NC}"
        echo "================================"
        
        # Inject to each active validator
        for port in "${active_ports[@]}"; do
            inject_tokens $port $TOKENS_PER_INJECTION
            sleep 1  # Small delay between validators
        done
        
        echo -e "${BLUE}📊 Status after round $i:${NC}"
        echo "   💰 Total tokens injected this round: $((${#active_ports[@]} * TOKENS_PER_INJECTION))"
        echo "   🏆 Cumulative total: $total_tokens_injected tokens"
        echo ""
        
        # Wait for next injection cycle
        if [ $i -lt $MAX_INJECTIONS ]; then
            echo "⏳ Waiting ${TOKEN_INJECTION_INTERVAL}s for next injection..."
            sleep $TOKEN_INJECTION_INTERVAL
        fi
    done
    
    echo -e "${GREEN}🎉 Token injection simulation completed!${NC}"
    echo "📊 Final Stats:"
    echo "   🔢 Total injections: $injection_count"
    echo "   💰 Total tokens injected: $total_tokens_injected"
    echo ""
}

# Function to run live monitoring
run_live_monitoring() {
    echo -e "${YELLOW}📡 Starting Live Monitoring Mode...${NC}"
    echo "Press Ctrl+C to stop"
    echo ""
    
    local monitor_count=0
    
    while true; do
        ((monitor_count++))
        
        echo -e "${BLUE}📊 Monitor Update #$monitor_count - $(date '+%H:%M:%S')${NC}"
        echo "================================================"
        
        # Monitor staking
        monitor_staking_status
        
        # Monitor network
        monitor_network_activity
        
        # Inject tokens periodically
        if [ $((monitor_count % 3)) -eq 0 ] && [ $injection_count -lt $MAX_INJECTIONS ]; then
            echo -e "${CYAN}💰 Periodic Token Injection...${NC}"
            
            # Get active ports
            local active_ports=($(netstat -tln 2>/dev/null | grep ":910[1-3] " | sed 's/.*:\(910[1-3]\) .*/\1/' | sort))
            
            if [ ${#active_ports[@]} -gt 0 ]; then
                # Inject to random validator
                local random_port=${active_ports[$RANDOM % ${#active_ports[@]}]}
                inject_tokens $random_port $TOKENS_PER_INJECTION
            fi
        fi
        
        echo "════════════════════════════════════════════════"
        echo ""
        
        # Wait for next monitoring cycle
        sleep $MONITOR_INTERVAL
    done
}

# Main menu
show_menu() {
    echo -e "${PURPLE}🎯 MMN Continuous Staking Monitor${NC}"
    echo "================================"
    echo ""
    echo "1. 🔥 Run Continuous Token Injection (Batch Mode)"
    echo "2. 📡 Live Monitoring with Periodic Injection"
    echo "3. 📊 Single Status Check"
    echo "4. 🧪 Run Integration Demo First"
    echo "5. ❌ Exit"
    echo ""
    echo -n "Choose option [1-5]: "
}

# Main function
main() {
    # Check if network is running
    local validators=$(ps aux | grep -E "(mmn node|bin/mmn node)" | grep -v grep | wc -l)
    
    if [ $validators -eq 0 ]; then
        echo -e "${YELLOW}⚠️  No MMN network detected${NC}"
        echo ""
        echo "Please start the network first:"
        echo "   ./scripts/build_and_test.sh"
        echo "   OR"
        echo "   ./scripts/test_network.sh"
        echo ""
    fi
    
    show_menu
    read choice
    
    case $choice in
        1)
            echo ""
            run_continuous_injection
            ;;
        2)
            echo ""
            run_live_monitoring
            ;;
        3)
            echo ""
            monitor_staking_status
            monitor_network_activity
            ;;
        4)
            echo ""
            echo "🧪 Running PoH + PoS Integration Demo..."
            go run poh_pos_integration_demo.go
            ;;
        5)
            echo "👋 Goodbye!"
            exit 0
            ;;
        *)
            echo "Invalid option. Please try again."
            main
            ;;
    esac
}

# Install bc if not available (for percentage calculations)
if ! command -v bc &> /dev/null; then
    echo "Installing bc for calculations..."
    sudo apt update && sudo apt install -y bc
fi

# Run main function
main
