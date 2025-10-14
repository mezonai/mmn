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
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/<port>/p2p/<peerID>"

Sync modes:
- Add flag `--sync` to join the network after syncing from a snapshot (fast catch-up). If you omit `--sync`, the node will do a full sync from genesis/block-by-block.

example:
MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node1" \
  --grpc-addr ":9001" \
  --listen-addr ":8001" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node1" \
  --grpc-addr ":9001" \
  --listen-addr ":8001" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2" \
  --sync

MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node2" \
  --listen-addr ":8002" \
  --grpc-addr ":9002" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node3" \
  --listen-addr ":8003" \
  --grpc-addr ":9003" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

Note: Faucet amount is now configured in the genesis configuration file (config/genesis.yml)

# Run with docker
## Build and run nodes
- To override configs inside `docker-compose.yaml`, create `.env` file with variables declared in `.env.example`
  ```
  docker compose build
  docker compose up
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

# Deploy
## bootstrap address
```
  --bootstrap-addresses \"/ip4/BOOTNODE_EXTERNAL_IP/udp/9000/quic-v1/p2p/BOOTNODE_PEER_ID\"
```
