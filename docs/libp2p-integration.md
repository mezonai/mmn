# MMN LibP2P Integration

## Overview

This document describes the LibP2P integration for MMN (Mezon Blockchain Network), which replaces the static gRPC-based networking with dynamic peer discovery and decentralized networking.

## Architecture

### Key Components

1. **P2P Service** (`p2p/service.go`) - Core LibP2P host and networking
2. **Network Adapter** (`p2p/adapter.go`) - Adapts LibP2P to MMN interfaces  
3. **Message Handlers** (`p2p/handlers.go`) - Handles different message types
4. **Broadcaster** (`p2p/broadcaster.go`) - Broadcasting utilities

### Network Protocols

- **Block Protocol**: `/mmn/block/1.0.0` - Direct peer-to-peer block transfer
- **Vote Protocol**: `/mmn/vote/1.0.0` - Consensus vote messages
- **Transaction Protocol**: `/mmn/tx/1.0.0` - Transaction propagation

### PubSub Topics

- `mmn-blocks` - Block announcements and broadcasting
- `mmn-votes` - Vote messages for consensus
- `mmn-transactions` - Transaction pool propagation

## Configuration

### Bootstrap Node Configuration

```yaml
# config/genesis.bootstrap.yml
config:
  p2p:
    listen_addrs:
      - "/ip4/0.0.0.0/tcp/4000"
      - "/ip4/0.0.0.0/tcp/4001/ws"  # WebSocket (optional)
    bootstrap_peers: []  # Bootstrap nodes don't need bootstrap peers
    enable_dht: true
    enable_pubsub: true
    is_bootstrap: true
    max_peers: 100
```

### Regular Node Configuration

```yaml
# config/genesis.node1.yml
config:
  p2p:
    listen_addrs:
      - "/ip4/0.0.0.0/tcp/4002"
      - "/ip4/0.0.0.0/tcp/4003/ws"
    bootstrap_peers:
      - "/ip4/127.0.0.1/tcp/4000/p2p/QmWpFGGTT6dLpagT71irC1s5Tn7u3KUWMZjbffA7DmxQut"  # Use actual Node ID
    enable_dht: true
    enable_pubsub: true
    is_bootstrap: false
    max_peers: 50
```

**Important**: Update the `bootstrap_peers` field with the actual Node ID from the bootstrap node output.

## Usage

### 1. Generate Node Keys

Before starting any nodes, you need to generate proper ED25519 keypairs:

```bash
# Generate bootstrap node key
cd /home/devops/mezonai/new/mmn
go run generate_bootstrap_key.go

# This will output:
# Private Key: a82e551172b115701aac05e2f2b17e8e55a36e1fb10fde52d5c322c5c5e628bd6eb256b49f718cd2f0456b9151b5700c0cf5db52c033ade5b8e31513a4f5fd84
# Public Key: 6eb256b49f718cd2f0456b9151b5700c0cf5db52c033ade5b8e31513a4f5fd84
# Bootstrap key written to config/bootstrap_key.txt

# Update genesis.bootstrap.yml with the generated public key
```

For other nodes, ensure their key files exist:
```bash
# Check if key files exist
ls -la config/key*.txt

# If missing, create them with proper ED25519 private keys (64-byte hex strings)
```

### 2. Start Bootstrap Node

```bash
# Start bootstrap node (will show Node ID in output)
./mmn-node -node bootstrap

# Note the Node ID from output, e.g.:
# Node ID: QmWpFGGTT6dLpagT71irC1s5Tn7u3KUWMZjbffA7DmxQut
```

### 3. Update Regular Node Configs

Update the bootstrap peer address in regular node configs with the actual Node ID:

```bash
# Edit config/genesis.node1.yml
# Update bootstrap_peers with actual Node ID:
bootstrap_peers:
  - "/ip4/127.0.0.1/tcp/4000/p2p/QmWpFGGTT6dLpagT71irC1s5Tn7u3KUWMZjbffA7DmxQut"
```

### 4. Start Regular Nodes

```bash
# Start node1
./mmn-node -node node1

# Start node2 
./mmn-node -node node2

# Nodes will automatically discover each other via DHT
```

### 5. Monitor Network

```bash
# Check peer connections
curl http://localhost:8090/p2p/info    # Bootstrap node
curl http://localhost:8091/p2p/info    # Node1

# Check network status  
curl http://localhost:8090/p2p/status  # Bootstrap node
curl http://localhost:8091/p2p/status  # Node1

# Health checks
curl http://localhost:8090/health      # Bootstrap node
curl http://localhost:8091/health      # Node1
```

## Key Generation Tool

The repository includes a key generation utility at `generate_bootstrap_key.go`:

```go
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	// Generate Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	privHex := hex.EncodeToString(priv)
	pubHex := hex.EncodeToString(pub)

	fmt.Printf("Private Key: %s\n", privHex)
	fmt.Printf("Public Key: %s\n", pubHex)

	// Write to bootstrap_key.txt
	err = os.WriteFile("config/bootstrap_key.txt", []byte(privHex), 0600)
	if err != nil {
		panic(err)
	}

	fmt.Println("Bootstrap key written to config/bootstrap_key.txt")
}
```

## Key Features

### 1. **Dynamic Peer Discovery**
- DHT-based peer discovery
- Bootstrap node for initial connections
- Automatic peer management and cleanup

### 2. **Redundant Communication**
- PubSub for broadcasting (gossip protocol)
- Direct streams for targeted messages
- Automatic failover between communication methods

### 3. **NAT Traversal**
- Built-in NAT port mapping
- AutoRelay support
- Connection management

### 4. **Security**
- Noise protocol for encryption
- Peer authentication
- Message validation

## Network Topology

```
Bootstrap Node (port 4000)
    ├── Node1 (port 4001)
    ├── Node2 (port 4002)  
    └── Node3 (port 4003)
         ├── Node4 (discovered via DHT)
         └── Node5 (discovered via DHT)
```

### Dynamic Discovery Flow

1. **Bootstrap Connection**: New nodes connect to bootstrap node
2. **DHT Join**: Node joins the DHT for peer discovery
3. **Peer Exchange**: Nodes discover each other through DHT
4. **Mesh Formation**: Nodes form a mesh network
5. **Consensus**: BFT consensus works over the P2P mesh

## Migration from gRPC

### Before (Static gRPC)
```go
// Static peer list in config
peers := []string{"node1:9001", "node2:9002", "node3:9003"}
grpcClient := network.NewGRPCClient(peers)
```

### After (Dynamic LibP2P)
```go
// Dynamic peer discovery
p2pService, _ := p2p.NewP2PService(&p2p.Config{
    BootstrapPeers: []string{"/ip4/127.0.0.1/tcp/4000/p2p/12D3..."},
    EnableDHT: true,
})
adapter := p2p.NewNetworkAdapter(p2pService)
```

## Performance Benefits

1. **Scalability**: No hardcoded peer limits
2. **Resilience**: Automatic reconnection and failover  
3. **Efficiency**: Gossip protocol reduces bandwidth
4. **Flexibility**: Support for various transports (TCP, WebSocket, QUIC)

## Development

### Running Tests

```bash
# Build the project first
cd /home/devops/mezonai/new/mmn
go build -o mmn-node cmd/main.go

# Generate bootstrap key
go run generate_bootstrap_key.go

# Start bootstrap node
./mmn-node -node bootstrap

# In another terminal, start regular nodes
./mmn-node -node node1
./mmn-node -node node2

# Test with multiple nodes locally - each on different ports
# Bootstrap: 4000, 4001
# Node1: 4002, 4003  
# Node2: 4004, 4005
```

### Debugging

```bash
# Enable LibP2P debug logging
export GOLOG_LOG_LEVEL=debug
export IPFS_LOGGING=debug
./mmn-node -node node1
```

## Step-by-Step Setup Guide

### Complete Setup from Scratch

1. **Build the project**:
   ```bash
   cd /home/devops/mezonai/new/mmn
   go mod tidy
   go build -o mmn-node cmd/main.go
   ```

2. **Generate bootstrap node key**:
   ```bash
   go run generate_bootstrap_key.go
   # This creates config/bootstrap_key.txt and shows the public key
   ```

3. **Update bootstrap config** with generated public key:
   ```bash
   # Edit config/genesis.bootstrap.yml
   # Replace pubkey field with generated public key
   ```

4. **Start bootstrap node**:
   ```bash
   ./mmn-node -node bootstrap
   # Note the Node ID from output (e.g., QmWpFGGTT6dLpagT71irC1s5Tn7u3KUWMZjbffA7DmxQut)
   ```

5. **Update regular node configs**:
   ```bash
   # Edit config/genesis.node1.yml
   # Update bootstrap_peers with actual Node ID
   bootstrap_peers:
     - "/ip4/127.0.0.1/tcp/4000/p2p/QmWpFGGTT6dLpagT71irC1s5Tn7u3KUWMZjbffA7DmxQut"
   ```

6. **Start regular nodes**:
   ```bash
   ./mmn-node -node node1
   ./mmn-node -node node2  # In separate terminals
   ```

7. **Verify connections**:
   ```bash
   curl http://localhost:8090/p2p/info  # Bootstrap node
   curl http://localhost:8091/p2p/info  # Node1  
   ```

## Troubleshooting

### Common Issues

1. **Bootstrap Connection Failed**
   - Check bootstrap node is running: `curl http://localhost:8090/health`
   - Verify bootstrap peer multiaddr format in config files
   - Check firewall/network connectivity on ports 4000-4003

2. **Key Generation Issues**
   - Ensure `generate_bootstrap_key.go` exists and is executable
   - Check that key files are created with proper permissions (0600)
   - Verify ED25519 private keys are 64-byte hex strings

3. **DHT Discovery Not Working**
   - Ensure `enable_dht: true` in config
   - Wait for DHT bootstrap (30-60 seconds)
   - Check NAT/firewall settings for P2P ports

4. **PubSub Messages Not Received** (FIXED)
   - This was resolved by properly managing topic lifecycle
   - Topics are now joined once at startup and reused for broadcasting
   - No more "topic already exists" errors

### Monitoring Commands

```bash
# Check LibP2P connections
curl http://localhost:8090/p2p/peers   # Bootstrap node
curl http://localhost:8091/p2p/peers   # Node1

# Network status
curl http://localhost:8090/p2p/status  # Bootstrap node  
curl http://localhost:8091/p2p/status  # Node1

# Health checks
curl http://localhost:8090/health      # Bootstrap node
curl http://localhost:8091/health      # Node1

# Consensus info
curl http://localhost:8090/consensus/info  # Bootstrap node
curl http://localhost:8091/consensus/info  # Node1
```

## Future Enhancements

1. **WebRTC Support** - Browser node connectivity
2. **QUIC Transport** - Improved performance
3. **Circuit Relay** - Enhanced NAT traversal
4. **Custom Protocols** - Application-specific protocols
5. **Metrics & Monitoring** - Prometheus integration

---

This LibP2P integration provides a robust, scalable foundation for MMN's networking layer, enabling true decentralized peer discovery and communication.
