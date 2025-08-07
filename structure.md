# MMN - Mezon Blockchain Network Architecture

## Tổng quan dự án

MMN (Mezon Blockchain Network) là một blockchain network sử dụng thuật toán đồng thuận **Proof of History (PoH)** kết hợp với **Byzantine Fault Tolerance (BFT)** voting. Dự án được xây dựng bằng Go và sử dụng gRPC để giao tiếp giữa các nodes.

## Kiến trúc tổng thể

### Core Components

1. **Proof of History (PoH)** - Tạo ra thứ tự thời gian cho các transaction
2. **Validator System** - Quản lý vai trò leader/follower và consensus
3. **Blockchain & Block Management** - Lưu trữ và quản lý blocks
4. **Transaction Processing** - Xử lý và validate transactions
5. **Network Layer** - Giao tiếp P2P giữa các nodes
6. **API Layer** - REST API cho client interactions

## Cấu trúc thư mục chi tiết

### Thư mục gốc
```
mmn/
├── cmd/                    # Entry point của ứng dụng
├── config/                 # Configuration files
├── api/                    # REST API server
├── block/                  # Block structure và operations
├── blockstore/             # Block storage và persistence
├── consensus/              # Consensus mechanism (voting)
├── ledger/                 # Account ledger và transaction processing
├── mempool/                # Transaction pool
├── network/                # gRPC networking
├── poh/                    # Proof of History implementation
├── proto/                  # Protocol Buffer definitions
├── validator/              # Validator logic (leader/follower)
├── utils/                  # Utility functions
├── pkg/                    # Shared packages
└── client_test/            # Test client
```

### 1. `/cmd/main.go` - Application Entry Point

**Chức năng chính:**
- Khởi tạo và cấu hình tất cả các components
- Load configuration từ genesis files
- Setup networking, PoH, validator, ledger
- Khởi chạy gRPC server và API server

**Flow khởi động:**
1. Load config từ `genesis.{node}.yml`
2. Khởi tạo BlockStore, Ledger, Collector
3. Setup PoH Engine với các tham số:
   - `hashesPerTick = 5`
   - `ticksPerSlot = 4` 
   - `tickInterval = 500ms`
4. Khởi tạo Validator với leader schedule
5. Start gRPC server và API server

### 2. `/poh/` - Proof of History

**Files:**
- `poh.go` - Core PoH engine
- `poh_recorder.go` - Record entries với PoH
- `poh_service.go` - Service layer
- `lead_schedule.go` - Leader scheduling
- `verify.go` - PoH verification

**Cơ chế hoạt động:**
- Tạo ra hash sequence liên tục để chứng minh thời gian
- Mỗi tick tạo ra một số lượng hash nhất định (`hashesPerTick`)
- Slot = nhiều ticks (`ticksPerSlot`)
- Leader được schedule theo slot

### 3. `/validator/` - Validator System

**Chức năng:**
- Quản lý vai trò Leader/Follower
- Leader: batch transactions, tạo blocks, broadcast
- Follower: nhận blocks, vote, validate
- Monitoring leader schedule và chuyển đổi role

**Key processes:**
- `handleEntry()` - Xử lý PoH entries và assemble blocks
- `leaderBatchLoop()` - Leader batch transactions từ mempool
- `roleMonitorLoop()` - Monitor và switch roles

### 4. `/consensus/` - Consensus Mechanism

**Files:**
- `vote.go` - Vote structure và signing
- `collector.go` - Collect và manage votes

**BFT Voting:**
- Validators vote trên blocks
- Threshold-based finalization
- Byzantine fault tolerance

### 5. `/block/` & `/blockstore/`

**Block Structure:**
```go
type Block struct {
    Slot      uint64          // Slot number
    PrevHash  [32]byte        // Previous block hash
    Entries   []poh.Entry     // PoH entries với transactions
    LeaderID  string          // Block creator
    Timestamp time.Time       // Creation time
    Hash      [32]byte        // Block hash
    Signature []byte          // Leader signature
    Status    BlockStatus     // Pending/Finalized
}
```

**BlockStore:**
- Persistent storage cho blocks
- File-based storage trong `/blockstore/blocks/`
- Support pending và finalized states

### 6. `/ledger/` - Account Ledger

**Features:**
- Account-based model (không phải UTXO)
- Balance tracking và nonce management
- Transaction history
- Write-Ahead Log (WAL) cho durability
- Snapshot mechanism

**Transaction Types:**
- `TxTypeFaucet` - Faucet transactions
- `TxTypeTransfer` - Normal transfers

### 7. `/mempool/` - Transaction Pool

**Chức năng:**
- Buffer transactions trước khi đưa vào blocks
- Pull batch cho leader processing
- Network propagation

### 8. `/network/` - Networking Layer

**gRPC Services:**
- `BlockService` - Block broadcast và subscription
- `VoteService` - Vote broadcasting
- `TxService` - Transaction propagation

**Components:**
- `server_grpc.go` - gRPC server implementation
- `client_grpc.go` - gRPC client cho peer communication

### 9. `/api/` - REST API

**Endpoints:**
- `POST /txs` - Submit transactions
- `GET /txs` - Get transaction history
- `GET /account` - Get account balance

### 10. `/config/` - Configuration

**Genesis Configuration:**
- Node definitions (pubkey, addresses)
- Leader schedule
- Faucet settings
- Peer addresses

**Files:**
- `genesis.yml` - Template
- `genesis.node1.yml`, `genesis.node2.yml`, `genesis.node3.yml` - Node-specific configs

### 11. `/proto/` - Protocol Buffers

**Definitions:**
- `block.proto` - Block structures
- `vote.proto` - Vote messages  
- `tx.proto` - Transaction messages

### 12. `/pkg/` - Shared Packages

**Extensive library:**
- `blockchain/` - Additional blockchain utilities
- `crypto/` - Cryptographic functions
- `common/` - Common utilities và types
- `rlp/` - RLP encoding (Ethereum-style)
- `storage/` - Database abstractions
- `p2p/` - P2P networking utilities

## Consensus Flow

### 1. Leader Phase
1. Leader pulls transactions từ mempool
2. Records transactions vào PoH entries
3. Assembles block khi slot boundary
4. Signs và broadcasts block
5. Self-votes trên block

### 2. Follower Phase  
1. Receives block từ leader
2. Validates block (PoH, signatures, transactions)
3. Casts vote nếu block valid
4. Broadcasts vote đến network

### 3. Finalization
1. Collector gathers votes
2. Khi đạt threshold → block finalized
3. Apply block đến ledger
4. Update account states

## Network Architecture

### Multi-Node Setup
- Mỗi node có unique identity (pubkey/privkey)
- Leader schedule rotates giữa nodes
- gRPC connections giữa all peers
- BFT consensus với vote thresholds

### Example 3-Node Network:
- Node 1: `localhost:8000` (gRPC), `localhost:8080` (API)
- Node 2: `localhost:8001` (gRPC), `localhost:8081` (API) 
- Node 3: `localhost:8002` (gRPC), `localhost:8082` (API)

## Key Features

### 1. **Proof of History**
- Tạo verifiable time ordering
- Giảm communication overhead
- Parallel transaction processing

### 2. **BFT Consensus**
- Byzantine fault tolerance
- Vote-based finalization
- Threshold-based security

### 3. **Account Model**
- Balance-based (không UTXO)
- Nonce-based replay protection
- Rich transaction history

### 4. **High Performance**
- Parallel PoH hashing
- Batched transaction processing
- Efficient networking với gRPC

## Development & Testing

### Build & Run
```bash
# Build
go build -o bin/mmn ./cmd

# Run nodes
go run cmd/main.go -node=node1
go run cmd/main.go -node=node2  
go run cmd/main.go -node=node3
```

### Client Testing
- TypeScript client trong `/client_test/`
- Ed25519 key generation
- Transaction signing và submission
- Balance checking

## Tương lai và mở rộng

### Implemented Features ✅
1. **LibP2P Integration** - Production-ready P2P networking
   - DHT-based peer discovery
   - PubSub for message broadcasting
   - Bootstrap node support
   - Dynamic node joining
2. **Smart Contracts** - WASM execution environment
3. **Sharding** - Horizontal scaling
4. **Light Clients** - Block header sync
5. **Advanced Storage** - BadgerDB optimization
6. **Cross-chain** - Interoperability protocols

### LibP2P Network Architecture

#### Bootstrap Node Model
- **Bootstrap Node**: Static, well-known nodes that help with initial peer discovery
- **Regular Nodes**: Dynamic nodes that join via bootstrap and DHT discovery
- **Peer Discovery**: DHT-based with gossip protocol for peer exchange

#### P2P Configuration
```yaml
p2p:
  listen_addrs:
    - "/ip4/0.0.0.0/tcp/4000"    # TCP listener
    - "/ip4/0.0.0.0/tcp/4001/ws" # WebSocket (optional)
  bootstrap_peers:
    - "/ip4/127.0.0.1/tcp/4000/p2p/12D3KooW..."  # Bootstrap node multiaddr
  enable_dht: true      # Enable DHT for peer discovery
  enable_pubsub: true   # Enable gossip for message broadcasting
  is_bootstrap: false   # Set to true for bootstrap nodes
  max_peers: 50        # Maximum number of peers
```

#### Network Protocols
- **Block Protocol**: `/mmn/block/1.0.0` - Block broadcasting and sync
- **Vote Protocol**: `/mmn/vote/1.0.0` - Consensus voting
- **Transaction Protocol**: `/mmn/tx/1.0.0` - Transaction propagation

#### PubSub Topics
- `mmn-blocks` - Block announcements
- `mmn-votes` - Vote messages
- `mmn-transactions` - Transaction propagation

### Running with LibP2P

#### Start Bootstrap Node
```bash
go run cmd/main.go -node=bootstrap
```

#### Join Network (Regular Nodes)
```bash
go run cmd/main.go -node=node1  # Connects to bootstrap automatically
go run cmd/main.go -node=node2  # Discovers peers via DHT
go run cmd/main.go -node=node3  # Dynamic peer discovery
```

#### Network Topology
```
Bootstrap Node (4000)
    ├── Node1 (4001) ──┐
    ├── Node2 (4002) ──┼── Mesh Network
    └── Node3 (4003) ──┘    (All nodes connect)
```

### Architecture Benefits
- **Scalability**: PoH enables parallel processing
- **Security**: BFT consensus với cryptographic proofs  
- **Performance**: Fast finality và high TPS
- **Flexibility**: Modular design cho easy extensions

---

Đây là một blockchain network hiện đại sử dụng những thuật toán tiên tiến nhất (PoH + BFT) để đạt được performance cao trong khi vẫn đảm bảo security và decentralization. Kiến trúc modular cho phép dễ dàng mở rộng và customize cho various use cases.
