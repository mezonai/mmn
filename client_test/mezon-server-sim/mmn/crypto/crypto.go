package crypto

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"mezon/v2/mmn/domain"
)

var ErrUnsupportedKey = errors.New("crypto: unsupported private key length")

func Serialize(tx *domain.Tx) []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	fmt.Println("Serialize metadata:", metadata)
	return []byte(metadata)
}

func SignTx(tx *domain.Tx, privKey []byte) (domain.SignedTx, error) {
	switch l := len(privKey); l {
	case ed25519.SeedSize:
		privKey = ed25519.NewKeyFromSeed(privKey)
	default:
		return domain.SignedTx{}, ErrUnsupportedKey
	}

	tx_hash := Serialize(tx)
	signature := ed25519.Sign(privKey, tx_hash)

	return domain.SignedTx{
		Tx:  tx,
		Sig: hex.EncodeToString(signature),
	}, nil
}

func Verify(tx *domain.Tx, sig string) bool {
	decoded, err := hex.DecodeString(tx.Sender)
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
