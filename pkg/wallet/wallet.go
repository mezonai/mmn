// File: pkg/wallet/wallet.go
package wallet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"cryptocypher/pkg/blockchain"
)

// Wallet represents a user's wallet with a private key and a public address.
type Wallet struct {
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
	Address    string // You can derive an address from the public key.
}

// NewWallet generates a new wallet.
func NewWallet() (*Wallet, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	pubKey := &privKey.PublicKey
	// For simplicity, let's use the hex encoding of the public key as the address.
	address := hex.EncodeToString(elliptic.Marshal(elliptic.P256(), pubKey.X, pubKey.Y))
	return &Wallet{
		PrivateKey: privKey,
		PublicKey:  pubKey,
		Address:    address,
	}, nil
}

// SignTransaction signs the given transaction using the wallet's private key.
func (w *Wallet) SignTransaction(tx *blockchain.Transaction) error {
	sig, err := blockchain.SignTransaction(tx, w.PrivateKey)
	if err != nil {
		return err
	}
	tx.Signature = sig
	return nil
}

// Display prints the wallet's details (avoid printing private key in production!).
func (w *Wallet) Display() {
	fmt.Println("Wallet Address:", w.Address)
	// For security reasons, do not expose the private key in production.
}
