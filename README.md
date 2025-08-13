<img width="1061" height="695" alt="1754016094020_image" src="https://github.com/user-attachments/assets/c2df9920-e7e6-48ed-baa3-994b281a7575" />


# run (dev)
go mod tidy

go run main.go run \
  --pubkey <public key> \
  --privkey-path <file path> \
  --listen-addr ":<port>" \
  --grpc-addr ":<port>" \
  --libp2p-addr "/ip4/0.0.0.0/tcp/<port>" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/<port>/p2p/<peerID>" \
  --faucet-address <faucet address> \
  --faucet-amount <amount>

example:
go run main.go run \
  --pubkey "6a4dd9b6efe0fc8f125be331735b0e33239e24f02c84e555ade9ea50bd1369db" \
  --privkey-path "config/key1.txt" \
  --listen-addr ":8001" \
  --grpc-addr ":9001" \
  --libp2p-addr "/ip4/0.0.0.0/tcp/10001" \
  --bootstrap-addresses "/ip4/127.0.0.1/tcp/10002/p2p/<peerID>" \
  --faucet-amount 2000000000

# run with docker
## run bootstrap node
docker compose -f docker-compose.bootstrap-node.yml build
docker compose -f docker-compose.bootstrap-node.yml up

Get the bootstrap node address -> bootstrap-addresses (docker-compose.yml)

## run nodes

docker compose build
docker compose up


# Build
go build -o bin/mmn ./cmd

# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.
