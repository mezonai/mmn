package client

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

var ErrUnsupportedKey = errors.New("crypto: unsupported private key length")

func Serialize(tx *Tx) []byte {
	amountStr := tx.Amount.String()
	metadata := fmt.Sprintf("%d|%s|%s|%s|%s|%d|%s", tx.Type, tx.Sender, tx.Recipient, amountStr, tx.TextData, tx.Nonce, tx.ExtraInfo)
	return []byte(metadata)
}

func SignTx(tx *Tx, pubKey, privKey []byte) (SignedTx, error) {
	switch l := len(privKey); l {
	case ed25519.SeedSize:
		privKey = ed25519.NewKeyFromSeed(privKey)
	default:
		return SignedTx{}, ErrUnsupportedKey
	}

	tx_hash := Serialize(tx)
	signature := ed25519.Sign(privKey, tx_hash)
	if tx.Type == TxTypeFaucet {
		return SignedTx{
			Tx:  tx,
			Sig: base58.Encode(signature),
		}, nil
	}

	userSig := UserSig{
		PubKey: pubKey,
		Sig:    signature,
	}
	userSigBytes, err := json.Marshal(userSig)
	if err != nil {
		return SignedTx{}, err
	}

	return SignedTx{
		Tx:  tx,
		Sig: base58.Encode(userSigBytes),
	}, nil
}

func Verify(tx *Tx, sig string) bool {
	tx_hash := Serialize(tx)
	if tx.Type == TxTypeFaucet {
		decoded, err := base58.Decode(tx.Sender)
		if err != nil {
			return false
		}

		if len(decoded) != ed25519.PublicKeySize {
			return false
		}

		pubKey := ed25519.PublicKey(decoded)
		signature, err := base58.Decode(sig)
		if err != nil {
			return false
		}

		if len(signature) != ed25519.SignatureSize {
			return false
		}

		return ed25519.Verify(pubKey, tx_hash, signature)
	}

	sigBytes, err := base58.Decode(sig)
	if err != nil {
		return false
	}

	var userSig UserSig
	if err := json.Unmarshal(sigBytes, &userSig); err != nil {
		return false
	}

	if len(userSig.PubKey) != ed25519.PublicKeySize || len(userSig.Sig) != ed25519.SignatureSize {
		return false
	}

	pubKey := ed25519.PublicKey(userSig.PubKey)

	return ed25519.Verify(pubKey, tx_hash, userSig.Sig)
}

func GenerateAddress(input string) string {
	sum := sha256.Sum256([]byte(input))
	return base58.Encode(sum[:])
}
