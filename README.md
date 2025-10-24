<img width="1061" height="695" alt="1754016094020_image" src="https://github.com/user-attachments/assets/c2df9920-e7e6-48ed-baa3-994b281a7575" />


# run (dev)
go mod tidy
### Run bootnode
MSYS_NO_PATHCONV=1 go run main.go bootnode \
  --privkey-path <file path> \
  --bootstrap-p2p-port 9000

Example:
MSYS_NO_PATHCONV=1 go run main.go bootnode \
  --privkey-path "./node-data/bootnode_privkey.txt" \
  --bootstrap-p2p-port 9000

### Run init node
go run main.go init \
  --data-dir <file folder> \
  --genesis "config/genesis.yml" \
  --database "leveldb" \
  --privkey-path <existing private key file> (optional)

Example with existing private key:
go run main.go init --data-dir "./node-data/node1" --genesis "config/genesis.yml" --database "leveldb"  --privkey-path "config/key1.txt"

go run main.go init --data-dir "./node-data/node2" --genesis "config/genesis.yml" --database "leveldb"  --privkey-path "config/key2.txt" 

go run main.go init --data-dir "./node-data/node3" --genesis "config/genesis.yml" --database "leveldb"  --privkey-path "config/key3.txt" 

### Run node
MSYS_NO_PATHCONV=1 go run main.go node \
  --privkey-path <file path> \
  --grpc-addr ":<port>" \
  --public-ip <public_ip> \
  --p2p-port <p2p_port> \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/<port>/p2p/<peerID>"

example:
MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node1" \
  --grpc-addr ":9001" \
  --listen-addr ":8001" \
  --public-ip "xx.xxx.xxx.xx" \
  --p2p-port "9090" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node2" \
  --listen-addr ":8002" \
  --grpc-addr ":9002" \
  --public-ip "xx.xxx.xxx.xx" \
  --p2p-port "9090" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node3" \
  --listen-addr ":8003" \
  --grpc-addr ":9003" \
  --public-ip "xx.xxx.xxx.xx" \
  --p2p-port "9090" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

Note: 
- Faucet amount is now configured in the genesis configuration file (config/genesis.yml)
- `--public-ip` flag is required for P2P advertising to external peers
- `--p2p-port` specifies the LibP2P port (use the same port for all nodes default: 9090)

# Run with docker
## Build and run nodes
- To override configs inside `docker-compose.yaml`, create `.env` file with variables declared in `.env.example`
  ```
  docker compose build
  docker compose up
  ```

## Environment Configuration

The project supports environment-based log level control to avoid excessive debug/trace logs in production:

### Environment Variables
- `LOGLEVEL`: Set log level (debug, info, warn, error). Default: info for production, debug for development

### Log Levels
- `debug`: Shows all log messages (DEBUG, INFO, WARN, ERROR)
- `info`: Shows INFO, WARN, ERROR messages (hides DEBUG)
- `warn`: Shows WARN, ERROR messages (hides DEBUG, INFO)
- `error`: Shows only ERROR messages (hides DEBUG, INFO, WARN)

Example `.env` file:
```
LOGLEVEL=info
LOGFILE_MAX_SIZE_MB=500
LOGFILE_MAX_AGE_DAYS=7
```

## Build & run with LevelDB

-Use direct command
  ```
  DB_VENDOR=leveldb docker compose up -d --build
  ```
-Use .env file
  - Create `.env` file in the root directory of source
    ```
    DB_VENDOR=leveldb
    ```
  - Run `docker compose up -d --build` to build and run nodes


# Build
go build -o bin/mmn ./cmd
## Run Bootnode
### Load private key from file
./mmn bootnode --privkey-path /path/to/privkey.txt --bootstrap-p2p-port 9000
### Generate random private key (default behavior)
./mmn bootnode --bootstrap-p2p-port 9000


# Perform transfer with CLI command
- Build executable mmn
  ```
  go build -o mmn .
  ```
- Then execute command to perform transfer to a wallet
  ```
  ./mmn transfer [-u <node-url>] [-t <recipient-addr>] [-a <amount>] [-p <sender-private-key>] [-f <sender-private-key-file>] [-v]
  ```
  For example:
  ```
  ./mmn transfer -v \
      -u localhost:9001 \
      -t EtgjD8gQLQhmSY1hpoVHdrEHyBEUBzkAU9PivA6NNSJx \
      -a 1000 \
      -p 302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee
  
  ./mmn transfer -v \
      -u localhost:9001 \
      -t EtgjD8gQLQhmSY1hpoVHdrEHyBEUBzkAU9PivA6NNSJx \
      -a 1000 \
      -f ./private.txt
  ```
- For more details about command, run `./mmn transfer --help`


# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.

## Monitoring stack (Grafana + Loki + Promtail + Prometheus)

- Create prometheus targets config file named `nodes.yaml` inside `./monitoring/prometheus/targets`, take a look at [example file](monitoring/prometheus/targets/nodes.example.yml)
- Open grafana at http://localhost:3300 (admin / admin)
- Take a look [Dashboard](http://localhost:3300/a/grafana-lokiexplore-app/explore) for node monitoring
- Navigate to [Drilldown > Logs](http://localhost:3300/a/grafana-lokiexplore-app/explore) for logs

# Multisig Faucet Testing

## Prerequisites
- Docker containers running with multisig addresses configured
- Genesis file updated with all node addresses in alloc section

## Test Commands

### 1. Get Multisig Address
```bash
./mmn multisig get-multisig-address
```

### 2. Add Proposer to Whitelist
```bash
./mmn multisig add-proposer --address "89y4uNijxzE9xXNvhU5oCbEN2RhSPCPQUwrJy7bPZPf8" --private-key-file "config/key2.txt"
```

### 3. Create Faucet Proposal
```bash
./mmn multisig create-proposal --multisig-addr "6grPYScpVpMS584AofGscikMFGoRG5dxCEFG5eJeTMap" --amount "40000" --message "Test faucet request" --private-key-file "config/key1.txt"
```

### 4. Approve Proposal (First Signature)
```bash
./mmn multisig approve --tx-hash "CD3WrdJ3X2PU5s7QkDL9wcnHDgezTu8KYyAwW69Ki9sq" --private-key-file "config/key2.txt"
```

### 5. Approve Proposal (Second Signature)
```bash
./mmn multisig approve --tx-hash "CD3WrdJ3X2PU5s7QkDL9wcnHDgezTu8KYyAwW69Ki9sq" --private-key-file "config/key3.txt"
```

### 6. Check Proposal Status
```bash
./mmn multisig status --tx-hash "CD3WrdJ3X2PU5s7QkDL9wcnHDgezTu8KYyAwW69Ki9sq" --private-key-file "config/key1.txt"
```

### 7. Get All Proposals
```bash
./mmn multisig get-proposals --private-key-file "config/key2.txt"
```

# Deploy
## bootstrap address
```
  --bootstrap-addresses \"/ip4/BOOTNODE_EXTERNAL_IP/udp/9000/quic-v1/p2p/BOOTNODE_PEER_ID\"
```
