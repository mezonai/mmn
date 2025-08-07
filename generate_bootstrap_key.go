package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	// Generate Ed25519 keypair
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}

	privHex := hex.EncodeToString(priv)
	pubHex := hex.EncodeToString(pub)

	fmt.Printf("Private Key: %s\n", privHex)
	fmt.Printf("Public Key: %s\n", pubHex)

	// Write to bootstrap_key.txt
	err = os.WriteFile("config/bootstrap_key.txt", []byte(privHex), 0600)
	if err != nil {
		panic(err)
	}

	fmt.Println("Bootstrap key written to config/bootstrap_key.txt")
}
