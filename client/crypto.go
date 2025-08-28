package client

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

var ErrUnsupportedKey = errors.New("crypto: unsupported private key length")

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

func Serialize(tx *Tx) []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	return []byte(metadata)
}

func Verify(tx *Tx, sig string, pubKeyBase58 string) bool {
	decoded, err := base58.Decode(pubKeyBase58)
	if err != nil || len(decoded) != ed25519.PublicKeySize {
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
