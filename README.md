<img width="1061" height="695" alt="1754016094020_image" src="https://github.com/user-attachments/assets/c2df9920-e7e6-48ed-baa3-994b281a7575" />


# run (dev)
go mod tidy

go run main.go node \
  --privkey-path <file path> \
  --grpc-addr ":<port>" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/<port>/p2p/<peerID>"

example:
go run main.go node \
  --privkey-path "config/key1.txt" \
  --grpc-addr ":9001" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/10002/p2p/<peerID>"

Note: Faucet amount is now configured in the genesis configuration file (config/genesis.yml)

# run with docker
## run nodes

docker compose build
docker compose up

# Build
go build -o bin/mmn ./cmd
## Run Bootnode
### Load private key from file
./mmn bootnode --privkey-path /path/to/privkey.txt --bootstrap-p2p-port 9000
### Generate random private key (default behavior)
./mmn bootnode --bootstrap-p2p-port 9000

# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.
