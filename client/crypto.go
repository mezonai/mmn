package client

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"filippo.io/edwards25519"

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

	txHash := Serialize(tx)
	signature := ed25519.Sign(privKey, txHash)
	if tx.Type == TxTypeTransferByKey {
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
	txHash := Serialize(tx)
	if tx.Type == TxTypeTransferByKey {
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

		return ed25519.Verify(pubKey, txHash, signature)
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

	return ed25519.Verify(pubKey, txHash, userSig.Sig)
}

func ValidateTxAddresses(tx *Tx) bool {
	// Only validate address for user content transactions
	if tx.Type != TxTypeUserContent {
		return true
	}

	var content UserContent
	if err := json.Unmarshal([]byte(tx.ExtraInfo), &content); err != nil {
		return false
	}

	// Only validate address for specific content types
	if !shouldValidateAddress(content.Type) {
		return true
	}

	recipientPK, err := base58.Decode(tx.Recipient)
	if err != nil {
		return false
	}

	if len(recipientPK) != addressDecodedExpectedLength {
		return false
	}

	if _, err = new(edwards25519.Point).SetBytes(recipientPK); err != nil {
		return false
	}

	return true
}

func GenerateAddress(input string) string {
	sum := sha256.Sum256([]byte(input))
	return base58.Encode(sum[:])
}

func shouldValidateAddress(txExtraInfoType string) bool {
	_, exists := txExtraInfoTypeNeedValidateAddress[txExtraInfoType]
	return exists
}
