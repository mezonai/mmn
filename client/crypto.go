package client

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
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
		Sig: hex.EncodeToString(signature),
	}, nil
}

func Serialize(tx *Tx) []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	return []byte(metadata)
}

func Verify(tx *Tx, sig string, pubKeyHex string) bool {
	decoded, err := hex.DecodeString(pubKeyHex)
	if err != nil {
		return false
	}
	pubKey := ed25519.PublicKey(decoded)
	tx_hash := Serialize(tx)
	signature, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}

	return ed25519.Verify(pubKey, tx_hash, signature)
}
