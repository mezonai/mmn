# MMN Blockchain: Bootstrap Node, Join Node và Consensus PoH

## Tổng quan

MMN (Mezon AI Network) là một blockchain sử dụng thuật toán đồng thuận Proof of History (PoH) kết hợp với libp2p cho mạng lưới P2P. Tài liệu này mô tả chi tiết quá trình khởi tạo bootstrap node, join node và cơ chế consensus.

## 1. Bootstrap Node

### 1.1 Mục đích
Bootstrap node là node đầu tiên trong mạng, đóng vai trò:
- Khởi tạo mạng DHT (Distributed Hash Table)
- Cung cấp điểm kết nối cho các node mới
- Quản lý discovery và routing trong mạng P2P

### 1.2 Kiến trúc Bootstrap Node

```
┌─────────────────────────────────────────┐
│              Bootstrap Node              │
├─────────────────────────────────────────┤
│  • Libp2p Host                          │
│  • DHT Server Mode                      │
│  • Connection Manager                   │
│  • Stream Handlers                      │
│  • Peer Discovery                       │
└─────────────────────────────────────────┘
```

### 1.3 Quy trình khởi tạo Bootstrap Node

#### Bước 1: Chuẩn bị Private Key
```bash
# Tạo private key (nếu chưa có)
./mmn node init --output-dir ./config

# Hoặc sử dụng private key có sẵn
./mmn bootnode --privkey-path ./config/bootnode_privkey.txt --bootstrap-p2p-port 9000
```

#### Bước 2: Tạo Libp2p Host
```go
// Trong bootstrap/bootstrap.go
func CreateNode(ctx context.Context, cfg *Config, bootstrapP2pPort string) (h host.Host, ddht *dht.IpfsDHT, err error) {
    // 1. Tạo Connection Manager
    lowWater := 80
    highWater := 100
    gracePeriod := time.Minute
    mgr, err := connmgr.NewConnManager(lowWater, highWater, connmgr.WithGracePeriod(gracePeriod))

    // 2. Cấu hình libp2p options
    options := []libp2p.Option{
        libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/" + bootstrapP2pPort),
        libp2p.EnableAutoNATv2(),
        libp2p.NATPortMap(),
        libp2p.EnableNATService(),
        libp2p.ForceReachabilityPublic(),
        libp2p.ConnectionManager(mgr),
        libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
            ddht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
            return ddht, nil
        }),
    }

    // 3. Tạo host với private key
    if cfg.PrivateKey != nil {
        options = append(options, libp2p.Identity(cfg.PrivateKey))
    }
    h, err = libp2p.New(options...)

    // 4. Bootstrap DHT
    if cfg.Bootstrap {
        if err := ddht.Bootstrap(ctx); err != nil {
            return nil, nil, err
        }
    }
}
```

#### Bước 3: Setup Stream Handlers và Notifications
```go
// Handler cho node info protocol
h.SetStreamHandler(NodeInfoProtocol, func(s network.Stream) {
    defer s.Close()
    info := map[string]interface{}{
        "new_peer_id": h.ID().String(),
        "addrs":       addrStrings(h.Addrs()),
    }
    data, _ := json.Marshal(info)
    s.Write(data)
})

// Network notification callbacks
h.Network().Notify(&network.NotifyBundle{
    ConnectedF: func(n network.Network, c network.Conn) {
        current := atomic.AddInt32(&ConnCount, 1)
        if current > MaxPeers {
            _ = c.Close()
            atomic.AddInt32(&ConnCount, -1)
            return
        }
        logx.Info("BOOTSTRAP NODE", "New peer connected:", c.RemotePeer())
    },
    DisconnectedF: func(n network.Network, c network.Conn) {
        atomic.AddInt32(&ConnCount, -1)
        logx.Info("BOOTSTRAP NODE", "Peer disconnected:", c.RemotePeer())
    },
})
```

### 1.4 Chạy Bootstrap Node
```bash
# Chạy bootstrap node với port 9000
./mmn bootnode --bootstrap-p2p-port 9000 --privkey-path ./config/bootnode_privkey.txt

# Output sẽ hiển thị multiaddress để các node khác kết nối:
# /ip4/192.168.1.100/tcp/9000/p2p/12D3KooWABC123...
```

## 2. Join Node Process

### 2.1 Kiến trúc Node

```
┌─────────────────────────────────────────┐
│               Regular Node               │
├─────────────────────────────────────────┤
│  • Libp2p Network Client                │
│  • DHT Client Mode                      │
│  • Blockstore                           │
│  • Mempool                              │
│  • PoH Engine                           │
│  • Validator                            │
│  • Ledger                               │
│  • API/gRPC Servers                     │
└─────────────────────────────────────────┘
```

### 2.2 Quy trình Join Node

#### Bước 1: Khởi tạo cấu hình
```bash
# Tạo private key cho node
./mmn node init --output-dir ./config

# Load cấu hình genesis
./mmn node --privkey-path ./config/privkey.txt \
    --bootstrap-addresses /ip4/192.168.1.100/tcp/9000/p2p/12D3KooWABC123... \
    --p2p-port 9001 \
    --listen-addr :8001 \
    --grpc-addr :9001 \
    --genesis-path ./config/genesis.yml
```

#### Bước 2: Khởi tạo các thành phần chính

```go
func runNode() {
    // 1. Load configuration
    cfg, err := loadConfiguration(genesisPath)
    
    // 2. Initialize blockstore
    bs, err := initializeBlockstore(pubKey, absLeveldbBlockDir, databaseBackend)
    
    // 3. Initialize ledger và blockchain
    ld := ledger.NewLedger(cfg.Faucet.Address)
    blockchain, err := initializeBlockchainWithGenesis(cfg, ld)
    
    // 4. Initialize PoH components
    _, pohService, recorder, err := initializePoH(cfg, pubKey, genesisPath)
    
    // 5. Initialize network
    libP2pClient, err := initializeNetwork(nodeConfig, bs, privKey)
    
    // 6. Initialize mempool
    mp, err := initializeMempool(libP2pClient, genesisPath)
    
    // 7. Initialize consensus collector
    collector := consensus.NewCollector(libP2pClient.GetPeersConnected() + 1)
    
    // 8. Setup network callbacks
    libP2pClient.SetupCallbacks(ld, privKey, nodeConfig, bs, collector, mp)
    
    // 9. Initialize validator
    val, err := initializeValidator(cfg, nodeConfig, pohService, recorder, mp, libP2pClient, bs, ld, collector, privKey, genesisPath)
    
    // 10. Start services
    startServices(cfg, nodeConfig, libP2pClient, ld, collector, val, bs, mp)
}
```

#### Bước 3: Network Discovery và Connection

```go
func NewNetWork(...) (*Libp2pNetwork, error) {
    // 1. Tạo libp2p host
    h, err := libp2p.New(
        libp2p.Identity(privKey),
        libp2p.ListenAddrStrings(listenAddr),
        libp2p.NATPortMap(),
        libp2p.EnableNATService(),
        libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
            ddht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
            return ddht, err
        }),
    )
    
    // 2. Bootstrap DHT để kết nối với mạng
    if err := ddht.Bootstrap(ctx); err != nil {
        return nil, fmt.Errorf("failed to bootstrap DHT: %w", err)
    }
    
    // 3. Setup discovery
    customDiscovery, err := discovery.NewDHTDiscovery(ctx, cancel, h, ddht, discovery.DHTConfig{})
    customDiscovery.Advertise(ctx, AdvertiseName)
    
    // 4. Setup pubsub
    ps, err := pubsub.NewGossipSub(ctx, h, pubsub.WithDiscovery(customDiscovery.GetRawDiscovery()))
    
    // 5. Setup stream handlers
    ln.setupHandlers(ctx, bootstrapPeers)
    
    // 6. Start discovery
    go ln.Discovery(customDiscovery, ctx, h)
}
```

## 3. Proof of History (PoH) Consensus

### 3.1 Kiến trúc PoH

```
┌─────────────────────────────────────────┐
│           PoH Architecture              │
├─────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────────┐   │
│  │ PoH Engine  │  │  PoH Recorder   │   │
│  │             │  │                 │   │
│  │ • AutoHash  │  │ • Track Entries │   │
│  │ • Tick Gen  │  │ • Tx Recording  │   │
│  │ • Hash      │  │ • Tick Hash     │   │
│  │   Chain     │  │   Tracking      │   │
│  └─────────────┘  └─────────────────┘   │
│           │                 │            │
│           └─────────────────┘            │
│                    │                     │
│  ┌─────────────────┴─────────────────┐   │
│  │         PoH Service              │   │
│  │                                  │   │
│  │ • Tick Interval Management       │   │
│  │ • Entry Collection & Flush       │   │
│  │ • Block Production Timing        │   │
│  └──────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

### 3.2 PoH Engine

#### Hash Chain Generation
```go
type Poh struct {
    Hash             [32]byte    // Current hash
    NumHashes        uint64      // Hash count
    HashesPerTick    uint64      // Hashes required per tick
    RemainingHashes  uint64      // Remaining hashes for current tick
    SlotStartTime    time.Time   // Slot start timestamp
    autoHashInterval time.Duration // Auto hash frequency
}

// Auto hash generation
func (p *Poh) AutoHash() {
    ticker := time.NewTicker(p.autoHashInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-p.stopCh:
            return
        case <-ticker.C:
            p.mu.Lock()
            if p.RemainingHashes > 1 {
                p.hashOnce(p.Hash[:])
            }
            p.mu.Unlock()
        }
    }
}

// Record transaction với PoH proof
func (p *Poh) Record(mixin [32]byte) *PohEntry {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    if p.RemainingHashes <= 1 {
        return nil // Cần tick trước
    }
    
    // Hash transaction với current hash
    p.hashOnce(append(p.Hash[:], mixin[:]...))
    
    entry := &PohEntry{
        NumHashes: p.NumHashes,
        Hash:      p.Hash,
        Tick:      false,
    }
    
    p.NumHashes = 0
    return entry
}

// Generate tick
func (p *Poh) Tick() *PohEntry {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    if p.RemainingHashes != 1 {
        return nil
    }
    
    p.hashOnce(p.Hash[:])
    
    entry := &PohEntry{
        NumHashes: p.NumHashes,
        Hash:      p.Hash,
        Tick:      true,
    }
    
    p.RemainingHashes = p.HashesPerTick
    p.NumHashes = 0
    return entry
}
```

### 3.3 PoH Recorder

```go
type PohRecorder struct {
    poh            *Poh
    leaderSchedule *LeaderSchedule
    myPubkey       string
    ticksPerSlot   uint64
    tickHeight     uint64
    entries        []Entry
    tickHash       map[uint64][32]byte
}

// Record transactions
func (r *PohRecorder) RecordTxs(txs [][]byte) (*Entry, error) {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    // Hash tất cả transactions
    mixin := HashTransactions(txs)
    
    // Record với PoH engine
    pohEntry := r.poh.Record(mixin)
    if pohEntry == nil {
        return nil, fmt.Errorf("PoH refused to record, tick required")
    }
    
    // Tạo entry với PoH proof
    entry := NewTxEntry(pohEntry.NumHashes, pohEntry.Hash, txs)
    r.entries = append(r.entries, entry)
    return &entry, nil
}

// Generate tick entry
func (r *PohRecorder) Tick() *Entry {
    r.mu.Lock()
    defer r.mu.Unlock()
    
    pohEntry := r.poh.Tick()
    if pohEntry == nil {
        return nil
    }
    
    entry := NewTickEntry(pohEntry.NumHashes, pohEntry.Hash)
    r.tickHash[r.tickHeight] = pohEntry.Hash
    r.tickHeight++
    return &entry
}
```

### 3.4 PoH Service - Timing Management

```go
type PohService struct {
    Recorder     *PohRecorder
    TickInterval time.Duration
    OnEntry      func(entry []Entry)
}

func (s *PohService) tickAndFlush() {
    ticker := time.NewTicker(s.TickInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            // Drain recorded entries
            entries := s.Recorder.DrainEntries()
            
            // Generate tick entry
            if tickEntry := s.Recorder.Tick(); tickEntry != nil {
                entries = append(entries, *tickEntry)
            }
            
            // Send entries to validator
            if len(entries) > 0 && s.OnEntry != nil {
                s.OnEntry(entries)
            }
        case <-s.stopCh:
            return
        }
    }
}
```

## 4. Validator và Block Production

### 4.1 Leader Schedule

```go
type Validator struct {
    Pubkey       string
    PrivKey      ed25519.PrivateKey
    Recorder     *poh.PohRecorder
    Service      *poh.PohService
    Schedule     *poh.LeaderSchedule
    Mempool      *mempool.Mempool
    TicksPerSlot uint64
    
    lastSlot          uint64
    leaderStartAtSlot uint64
    collectedEntries  []poh.Entry
    collector         *consensus.Collector
}

// Xử lý entry từ PoH Service
func (v *Validator) handleEntry(entries []poh.Entry) {
    v.collectedEntries = append(v.collectedEntries, entries...)
    
    currentSlot := v.Recorder.CurrentSlot()
    
    // Check if we are leader for current slot
    if v.Schedule.IsLeader(v.Pubkey, currentSlot) {
        if v.leaderStartAtSlot == NoSlot {
            v.onLeaderSlotStart(currentSlot)
        }
        v.onLeaderSlotTick(currentSlot)
    } else {
        v.onFollowerSlot(currentSlot)
    }
}

// Leader slot processing
func (v *Validator) onLeaderSlotTick(currentSlot uint64) {
    // Collect transactions from mempool
    txs := v.Mempool.GetTxsForBatch(v.BatchSize)
    
    if len(txs) > 0 {
        // Record transactions với PoH
        entry, err := v.Recorder.RecordTxs(txs)
        if err == nil {
            v.collectedEntries = append(v.collectedEntries, *entry)
        }
    }
    
    // Check if slot is complete
    if v.isSlotComplete(currentSlot) {
        v.finalizeLeaderSlot(currentSlot)
    }
}

// Finalize block production
func (v *Validator) finalizeLeaderSlot(slot uint64) {
    // Create block from collected entries
    block := v.createBlockFromEntries(slot, v.collectedEntries)
    
    // Apply block to ledger
    session := v.ledger.NewSession()
    err := v.applyBlockToSession(block, session)
    if err != nil {
        return
    }
    
    // Commit session
    v.session = session
    
    // Store block
    v.blockStore.Store(block)
    
    // Broadcast block to network
    v.netClient.BroadcastBlock(block)
    
    // Reset for next slot
    v.collectedEntries = make([]poh.Entry, 0)
    v.leaderStartAtSlot = NoSlot
}
```

### 4.2 Follower Validation

```go
func (v *Validator) onFollowerSlot(currentSlot uint64) {
    // Wait for block from leader
    // Validate received block
    // Apply to ledger if valid
}

func (v *Validator) validateBlock(block *block.Block) error {
    // 1. Validate PoH entries sequence
    if !v.validatePohSequence(block.Entries) {
        return fmt.Errorf("invalid PoH sequence")
    }
    
    // 2. Validate transactions
    for _, entry := range block.Entries {
        if !entry.IsTick() {
            for _, tx := range entry.Transactions {
                if err := v.validateTransaction(tx); err != nil {
                    return err
                }
            }
        }
    }
    
    // 3. Validate leader signature
    if !v.validateLeaderSignature(block) {
        return fmt.Errorf("invalid leader signature")
    }
    
    return nil
}
```

## 5. Consensus Flow

### 5.1 Quy trình tổng quát

```
1. Bootstrap Node khởi tạo mạng
   ↓
2. Node join vào mạng thông qua bootstrap
   ↓
3. Node sync genesis state và leader schedule
   ↓
4. PoH Engine bắt đầu auto hash
   ↓
5. Leader theo lịch bắt đầu thu thập transactions
   ↓
6. Record transactions với PoH proof
   ↓
7. Tạo block từ entries và broadcast
   ↓
8. Followers validate và apply block
   ↓
9. Slot chuyển đổi theo schedule
```

### 5.2 Timing và Configuration

```yaml
# genesis.yml
genesis:
  faucet:
    address: "faucet_address"
    amount: 1000000000

poh:
  hashes_per_tick: 1000
  ticks_per_slot: 64
  tick_interval_ms: 100

validator:
  leader_timeout: 5000
  leader_timeout_loop_interval: 1000
  batch_size: 100

mempool:
  max_txs: 10000

leader_schedule:
  - pubkey: "leader1_pubkey"
    slots: [0, 2, 4]
  - pubkey: "leader2_pubkey"
    slots: [1, 3, 5]
```

## 6. Deployment và Monitoring

### 6.1 Bootstrap Node Deployment

```bash
# 1. Tạo private key cho bootstrap
./mmn node init --output-dir ./bootstrap

# 2. Chạy bootstrap node
./mmn bootnode \
  --bootstrap-p2p-port 9000 \
  --privkey-path ./bootstrap/privkey.txt

# 3. Lấy multiaddress để cấu hình cho các node khác
# Output: /ip4/<IP>/tcp/9000/p2p/<PeerID>
```

### 6.2 Regular Node Deployment

```bash
# 1. Tạo private key
./mmn node init --output-dir ./node1

# 2. Chạy node
./mmn node \
  --privkey-path ./node1/privkey.txt \
  --bootstrap-addresses /ip4/<BOOTSTRAP_IP>/tcp/9000/p2p/<BOOTSTRAP_PEER_ID> \
  --p2p-port 9001 \
  --listen-addr :8001 \
  --grpc-addr :9001 \
  --genesis-path ./config/genesis.yml \
  --database leveldb
```

### 6.3 Docker Deployment

```yaml
# docker-compose.yml
version: '3.8'

services:
  bootstrap:
    build: .
    command: >
      mmn bootnode
      --bootstrap-p2p-port 9000
      --privkey-path /config/bootnode_privkey.txt
    ports:
      - "9000:9000"
    volumes:
      - ./config:/config

  node1:
    build: .
    command: >
      mmn node
      --privkey-path /config/key1.txt
      --bootstrap-addresses /ip4/bootstrap/tcp/9000/p2p/12D3KooW...
      --p2p-port 9001
      --listen-addr :8001
      --grpc-addr :9001
      --genesis-path /config/genesis.yml
    depends_on:
      - bootstrap
    ports:
      - "8001:8001"
      - "9001:9001"
```

## 7. Troubleshooting

### 7.1 Các vấn đề thường gặp

1. **Bootstrap connection failed**
   - Kiểm tra network connectivity
   - Xác minh multiaddress format
   - Check firewall settings

2. **PoH sync issues**
   - Kiểm tra tick interval configuration
   - Verify hash chain integrity
   - Check slot timing

3. **Block validation failed**
   - Verify transaction signatures
   - Check PoH sequence
   - Validate leader schedule

### 7.2 Monitoring Commands

```bash
# Check node status
curl http://localhost:8001/status

# Check peers
curl http://localhost:8001/peers

# Check mempool
curl http://localhost:8001/mempool

# Check blocks
curl http://localhost:8001/blocks
```

## Kết luận

MMN blockchain sử dụng kiến trúc tiên tiến với PoH consensus kết hợp PoS để đảm bảo thời gian khối nhanh và đáng tin cậy. Quá trình bootstrap và join node được thiết kế để dễ dàng mở rộng mạng, trong khi cơ chế PoH+PoS cung cấp proof of time và stake-based security không cần đồng bộ clock giữa các node.

## 8. Proof of Stake (PoS) Integration

### 8.1 Tổng quan PoH + PoS

MMN blockchain kết hợp Proof of History (PoH) với Proof of Stake (PoS) để tạo ra một consensus mechanism mạnh mẽ:

- **PoH**: Cung cấp thứ tự thời gian cryptographic và timing cho blocks
- **PoS**: Xác định ai được quyền sản xuất block dựa trên stake weight
- **Equal Distribution**: Với 10 validators thì mỗi validator nhận 10% slots

### 8.2 Kiến trúc Staking System

```
┌─────────────────────────────────────────────────────────────┐
│                    Staking Architecture                      │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │ Stake Pool   │  │ Scheduler    │  │ Stake Validator  │  │
│  │              │  │              │  │                  │  │
│  │ • Validators │  │ • Leader     │  │ • PoH + PoS      │  │
│  │ • Delegators │  │   Schedule   │  │ • Performance    │  │
│  │ • Rewards    │  │ • Epoch Mgmt │  │   Tracking       │  │
│  │ • Stake Mgmt │  │ • Equal Dist │  │ • Block Prod     │  │
│  └──────────────┘  └──────────────┘  └──────────────────┘  │
│          │                 │                    │           │
│          └─────────────────┼────────────────────┘           │
│                            │                                │
│  ┌─────────────────────────┴────────────────────────────┐   │
│  │              Stake Manager                           │   │
│  │                                                      │   │
│  │ • Transaction Processing                             │   │
│  │ • Epoch Transitions                                  │   │
│  │ • Reward Distribution                                │   │
│  │ • Validator Registry                                 │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 8.3 Staking Configuration

```yaml
# genesis_with_staking.yml
config:
  staking:
    enabled: true
    slots_per_epoch: 8640        # ~1 hour epochs
    min_stake_amount: "1000000"  # Minimum stake required
    max_validators: 1000         # Maximum validators
    epoch_reward_amount: "100000000"  # Rewards per epoch
    unstake_cooldown: 432000     # ~3 days cooldown
    activation_delay: 8640       # ~1 hour activation

  genesis_validators:
    - pubkey: "validator1_pubkey"
      stake_amount: "10000000"   # Hard-coded stake
      commission: 5              # 5% commission

    - pubkey: "validator2_pubkey"
      stake_amount: "10000000"   # Equal stakes
      commission: 10             # Different commissions

    # ... up to 10 validators for equal 10% distribution
```

### 8.4 Validator Registration và Staking

#### Registration Process
```go
// Register validator with initial stake
func RegisterValidator(pubkey string, stakeAmount *big.Int, commission uint8) error {
    // Validate minimum stake
    if stakeAmount.Cmp(minStakeAmount) < 0 {
        return errors.New("stake below minimum")
    }
    
    // Create validator
    validator := &ValidatorInfo{
        Pubkey:         pubkey,
        StakeAmount:    stakeAmount,
        Commission:     commission,
        State:          StakeStateActivating,
        ActivationSlot: currentSlot + activationDelay,
    }
    
    // Add to stake pool
    stakePool.validators[pubkey] = validator
    totalStake.Add(totalStake, stakeAmount)
    
    return nil
}
```

#### Delegation Process
```go
// Delegate stake to validator
func Delegate(delegatorPubkey, validatorPubkey string, amount *big.Int) error {
    validator := stakePool.validators[validatorPubkey]
    
    // Add delegation
    if existing, exists := validator.Delegators[delegatorPubkey]; exists {
        existing.Add(existing, amount)
    } else {
        validator.Delegators[delegatorPubkey] = amount
    }
    
    // Update total stake
    validator.StakeAmount.Add(validator.StakeAmount, amount)
    totalStake.Add(totalStake, amount)
    
    return nil
}
```

### 8.5 Equal Stake Distribution

Theo yêu cầu, với 10 validators thì mỗi validator nhận 10% slots:

```go
// GetStakeDistribution returns equal distribution
func (sp *StakePool) GetStakeDistribution() map[string]float64 {
    activeValidators := sp.GetActiveValidators()
    distribution := make(map[string]float64)
    
    if len(activeValidators) == 0 {
        return distribution
    }
    
    // Equal distribution: 1/n for each active validator
    weight := 1.0 / float64(len(activeValidators))
    for pubkey := range activeValidators {
        distribution[pubkey] = weight  // 10% for 10 validators
    }
    
    return distribution
}
```

### 8.6 Leader Schedule Generation

```go
// Generate equal leader schedule
func (sws *StakeWeightedScheduler) GenerateLeaderSchedule(epoch uint64, seed []byte) (*poh.LeaderSchedule, error) {
    activeValidators := sws.stakePool.GetActiveValidators()
    distribution := sws.stakePool.GetStakeDistribution()
    
    // Equal slots per validator
    slotsPerValidator := sws.slotsPerEpoch / uint64(len(validators))
    remainingSlots := sws.slotsPerEpoch % uint64(len(validators))
    
    epochStartSlot := epoch * sws.slotsPerEpoch
    
    for i, validator := range validators {
        slotCount := slotsPerValidator
        
        // Distribute remaining slots evenly
        if uint64(i) < remainingSlots {
            slotCount++
        }
        
        startSlot := epochStartSlot + slotsAssigned
        endSlot := startSlot + slotCount - 1
        
        entries = append(entries, poh.LeaderScheduleEntry{
            StartSlot: startSlot,
            EndSlot:   endSlot,
            Leader:    validator,
        })
    }
    
    return poh.NewLeaderSchedule(entries)
}
```

### 8.7 Epoch Management

```go
// Epoch transition process
func (sm *StakeManager) transitionToNewEpoch(newEpoch, currentSlot uint64) {
    // 1. Distribute rewards for completed epoch
    sm.distributeEpochRewards(sm.currentEpoch, currentSlot-1)
    
    // 2. Generate new schedule with equal distribution
    newSeed := sm.generateEpochSeed(newEpoch)
    schedule, err := sm.scheduler.GenerateLeaderSchedule(newEpoch, newSeed)
    
    // 3. Update state
    sm.currentSchedule = schedule
    sm.currentEpoch = newEpoch
    sm.epochSeed = newSeed
    sm.stakePool.UpdateEpoch(newEpoch)
}

// Equal reward distribution
func (sp *StakePool) DistributeRewards(epochRewards *big.Int, slot uint64) map[string]*big.Int {
    rewards := make(map[string]*big.Int)
    activeValidators := sp.getActiveValidatorsUnsafe()
    
    if len(activeValidators) == 0 {
        return rewards
    }
    
    // Equal distribution among active validators
    rewardPerValidator := new(big.Int).Div(epochRewards, big.NewInt(int64(len(activeValidators))))
    
    for pubkey, validator := range activeValidators {
        // Validator commission
        commission := new(big.Int).Mul(rewardPerValidator, big.NewInt(int64(validator.Commission)))
        commission.Div(commission, big.NewInt(100))
        
        rewards[pubkey] = commission
        
        // Delegate rewards proportionally
        delegatorRewards := new(big.Int).Sub(rewardPerValidator, commission)
        // ... distribute to delegators based on their stake proportion
    }
    
    return rewards
}
```

### 8.8 Stake Validator Integration

```go
// StakeValidator extends PoH with PoS
func (v *StakeValidator) handleEntry(entries []poh.Entry) {
    v.collectedEntries = append(v.collectedEntries, entries...)
    currentSlot := v.Recorder.CurrentSlot()
    
    // Check leadership using stake manager
    if v.stakeManager.IsLeaderForSlot(v.Pubkey, currentSlot) {
        if v.leaderStartAtSlot == NoSlot {
            v.onLeaderSlotStart(currentSlot)
        }
        v.onLeaderSlotTick(currentSlot)
    } else {
        v.onFollowerSlot(currentSlot)
    }
}

// Generate stake proof for block
func (v *StakeValidator) createBlockFromEntries(slot uint64, entries []poh.Entry) *block.Block {
    block := &block.Block{
        Header: block.Header{
            Slot:       slot,
            Timestamp:  time.Now().Unix(),
            Producer:   v.Pubkey,
        },
        Entries: entries,
    }
    
    // Add stake proof
    proof, err := v.stakeManager.stakePool.GenerateStakeProof(v.Pubkey, slot, v.PrivKey)
    if err == nil {
        block.Header.StakeProof = proof
    }
    
    return block
}
```

### 8.9 Staking API Endpoints

```bash
# Get all validators
curl http://localhost:8001/validators

# Get specific validator
curl http://localhost:8001/validators/{pubkey}

# Get active validators
curl http://localhost:8001/validators/active

# Get stake pool stats
curl http://localhost:8001/stake/pool

# Get stake distribution
curl http://localhost:8001/stake/distribution

# Get epoch information
curl http://localhost:8001/stake/epoch

# Get current leader schedule
curl http://localhost:8001/schedule/current

# Get leader for specific slot
curl http://localhost:8001/schedule/slot/{slot}

# Delegate stake (POST)
curl -X POST http://localhost:8001/stake/delegate \
  -H "Content-Type: application/json" \
  -d '{
    "delegator_pubkey": "delegator_key",
    "validator_pubkey": "validator_key", 
    "amount": "1000000",
    "signature": "signature_hex"
  }'

# Undelegate stake (POST)
curl -X POST http://localhost:8001/stake/undelegate \
  -H "Content-Type: application/json" \
  -d '{
    "delegator_pubkey": "delegator_key",
    "validator_pubkey": "validator_key",
    "amount": "500000", 
    "signature": "signature_hex"
  }'
```

### 8.10 Running Node with Staking

```bash
# 1. Generate validators keys (do this for each validator)
./mmn node init --output-dir ./validator1
./mmn node init --output-dir ./validator2
# ... up to validator10

# 2. Update genesis_with_staking.yml with actual pubkeys
# Replace "validator1_pubkey" etc with actual public keys

# 3. Run bootstrap node
./mmn bootnode --bootstrap-p2p-port 9000 --privkey-path ./config/bootnode_privkey.txt

# 4. Run validator nodes with staking enabled
./mmn node \
  --privkey-path ./validator1/privkey.txt \
  --bootstrap-addresses /ip4/bootstrap_ip/tcp/9000/p2p/bootstrap_peer_id \
  --p2p-port 9001 \
  --listen-addr :8001 \
  --grpc-addr :9001 \
  --genesis-path ./config/genesis_with_staking.yml \
  --database leveldb

# Repeat for all 10 validators with different ports
```

### 8.11 Monitoring Staking

```bash
# Check validator performance
curl http://localhost:8001/validators/validator1_pubkey

# Monitor epoch transitions
curl http://localhost:8001/stake/epoch

# Check stake distribution
curl http://localhost:8001/stake/distribution

# View current leader schedule
curl http://localhost:8001/schedule/current

# Example response:
{
  "epoch": 5,
  "start_slot": 43200,
  "end_slot": 51839,
  "schedule": [
    {
      "start_slot": 43200,
      "end_slot": 44063,
      "leader": "validator1_pubkey"
    },
    {
      "start_slot": 44064,
      "end_slot": 44927,
      "leader": "validator2_pubkey"
    }
    // ... equal distribution for all 10 validators
  ]
}
```

### 8.12 Consensus Flow với PoH + PoS

```
1. Bootstrap Node khởi tạo mạng
   ↓
2. Validators join và đăng ký stake
   ↓  
3. Genesis validators được activate
   ↓
4. Stake Manager tạo equal leader schedule (10% mỗi validator)
   ↓
5. PoH Engine bắt đầu auto hash
   ↓
6. Leader theo schedule PoS thu thập transactions
   ↓
7. Record transactions với PoH proof + Stake proof
   ↓
8. Tạo block và broadcast với dual proof
   ↓
9. Followers validate PoH sequence + Stake proof
   ↓
10. Epoch transition → redistribute rewards → new schedule
```

## 9. Performance và Security

### 9.1 Performance Benefits
- **Fast Finality**: PoH provides cryptographic proof of time ordering
- **Scalability**: Equal distribution prevents stake centralization
- **Predictable Timing**: 400ms slots with deterministic leader schedule

### 9.2 Security Features
- **Dual Proof**: Both PoH and stake proof required for blocks
- **Slashing Protection**: Performance tracking for future slashing implementation
- **Economic Security**: Validators have economic stake in network health

### 9.3 Validator Performance Tracking
```go
// Performance metrics tracked per validator
type PerformanceMetrics struct {
    SlotsProduced    uint64  // Successful slot productions
    SlotsMissed      uint64  // Missed slot productions  
    BlocksProduced   uint64  // Total blocks produced
    PerformanceRate  float64 // Success rate (0.0 - 1.0)
}
```
