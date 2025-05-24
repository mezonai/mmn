<p align="center">
  <img  alt="docker for dummies" height="128px" width="128px" src="https://miro.medium.com/max/1200/1*i2skbfmDsHayHhqPfwt6pA.png">
</p>

# Blockchain Implementation in Golang

A blockchain implementation in Go, demonstrating essential concepts of blockchain technology. This project includes basic block structures, proof-of-work consensus, cryptographic transaction signing, and block verification.

## Features
- **Block Structure**: Each block holds data, a hash, a previous hash link, a nonce, and transactions.
- **Proof of Work (PoW)**: Implements a proof-of-work system using md5 hashing to maintain blockchain integrity.
- **Wallets and Transactions**: Supports RSA key pairs for wallets, transaction creation, signing, and verification.
- **Genesis Block**: Automatically creates the genesis (first) block with a Coinbase transaction.
- **CLI Demo**: Demonstrates the creation of blocks, transactions, and verification on the blockchain.

## Installation

### Prerequisites
- **Go** version 1.16 or higher.

### Setup
- Clone the repository:

```
git clone https://github.com/thesaltree/blockchain-golang.git
cd blockchain-golang
go mod tidy
```

- Run the project:

```
go run main.go
```

