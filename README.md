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

example:
MSYS_NO_PATHCONV=1 go run main.go node \
  --data-dir "./node-data/node1" \
  --grpc-addr ":9001" \
  --listen-addr ":8001" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/9000/p2p/12D3KooWAhZyyZV2KBtfm8zsLaKPvcmVfaYczJ5UdpB8cJU7vKg2"

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


# Perform transfer from faucet with CLI command
- If node runs inside docker container with docker compose, execute the following command first
  ```
  docker compose exec -it <node_service_name> bash
  ```
  Or directly access container
  ```
  docker exec -it <node_container> bash
  ```
- Then execute command to perform transfer from faucet to wallet
  ```
  ./mmn faucet fund [--verbose] [--p <private-key>] [--f <private-key-file>] [--u <node-url>] <address> <amount>
  ```
  For example:
  ```
  ./mmn faucet fund EtgjD8gQLQhmSY1hpoVHdrEHyBEUBzkAU9PivA6NNSJx 1000 -v -f /path/to/private -u node1:9001
  ```
- For more details about command, run `./mmn faucet fund --help`


# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.
