package main

import (
	"crypto/ed25519"
)

// Wallet represents a user wallet with public/private key pair
type Wallet struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
	Address    string
}

// Tx represents a transaction structure
type Tx struct {
	Type      int    `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    uint64 `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce"`
}

// SignedTx represents a signed transaction
type SignedTx struct {
	Tx  *Tx
	Sig string
}
