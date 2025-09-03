package client

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

var ErrUnsupportedKey = errors.New("crypto: unsupported private key length")

func Serialize(tx *Tx) []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	fmt.Println("Serialize metadata:", metadata)
	return []byte(metadata)
}

func SignTx(tx *Tx, privKey []byte) (SignedTx, error) {
	switch l := len(privKey); l {
	case ed25519.SeedSize:
		privKey = ed25519.NewKeyFromSeed(privKey)
	default:
		return SignedTx{}, ErrUnsupportedKey
	}

	tx_hash := Serialize(tx)
	signature := ed25519.Sign(privKey, tx_hash)

	return SignedTx{
		Tx:  tx,
		Sig: base58.Encode(signature),
	}, nil
}

func Verify(tx *Tx, sig string) bool {
	decoded, err := base58.Decode(tx.Sender)
	if err != nil {
		return false
	}
	pubKey := ed25519.PublicKey(decoded)
	tx_hash := Serialize(tx)
	signature, err := base58.Decode(sig)
	if err != nil {
		return false
	}

	return ed25519.Verify(pubKey, tx_hash, signature)
}
