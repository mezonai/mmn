<img width="1063" height="702" alt="1753958148000_image" src="https://github.com/user-attachments/assets/0e2ac8bf-79e0-4cef-886f-89e0f30e29cd" />

# run (dev)
go mod tidy

Node 1 (first nodes):
go run cmd/main.go -rpcPort="8080" -listenAddress="localhost:8000" -peerAddresses="localhost:8001"

Node 2:
go run cmd/main.go -rpcPort="8081" -listenAddress="localhost:8001" -peerAddresses="localhost:8000"

# Build
go build -o bin/mmn ./cmd

# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.
