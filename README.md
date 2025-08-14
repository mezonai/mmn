<img width="1061" height="695" alt="1754016094020_image" src="https://github.com/user-attachments/assets/c2df9920-e7e6-48ed-baa3-994b281a7575" />


# run (dev)
go mod tidy

go run cmd/main.go --node <node name> 

example:
go run cmd/main.go --node node3

# Build
go build -o bin/mmn ./cmd

# uses cases
Mezon -> (auto gen wallet) => user has a wallet
Mezon (wallet) -> create and sign transaction -> send rpc -> mmn node verify commit and broadcast to nodes.
