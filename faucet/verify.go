package faucet

import (
	"crypto/ed25519"
	"encoding/hex"

	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/zkverify"
	"github.com/mr-tron/base58"
)

func VerifySignature(message string, signatureHex string, signerPubKey string, zk *zkverify.ZkVerify, zkProof string, zkPub string) bool {
	if zk != nil && zkProof != "" && zkPub != "" {
		sigBytes, err := hex.DecodeString(signatureHex)
		if err != nil {
			return false
		}

		var userSig UserSig
		if err := jsonx.Unmarshal(sigBytes, &userSig); err != nil {
			return false
		}
		if len(userSig.PubKey) != ed25519.PublicKeySize || len(userSig.Sig) == 0 {
			return false
		}

		if !ed25519.Verify(userSig.PubKey, []byte(message), userSig.Sig) {
			return false
		}

		derived := common.EncodeBytesToBase58(userSig.PubKey)
		return zk.Verify(signerPubKey, derived, zkProof, zkPub)
	}

	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	pubKeyBytes, err := base58.Decode(signerPubKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pubKeyBytes), []byte(message), sigBytes)
}
