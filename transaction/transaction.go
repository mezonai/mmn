package transaction

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/zkverify"
)

const (
	TxTypeTransfer = 0
	TxTypeFaucet   = 1
)

const ADD_FAUCET_MESSAGE = "FAUCET_ACTION:ADD_SIGNATURE"

type Transaction struct {
	Type      int32        `json:"type"`
	Sender    string       `json:"sender"`
	Recipient string       `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64       `json:"timestamp"`
	TextData  string       `json:"text_data"`
	Nonce     uint64       `json:"nonce,omitempty"`
	Signature string       `json:"signature,omitempty"`
	ExtraInfo string       `json:"extra_info,omitempty"`
	ZkProof   string       `json:"zk_proof,omitempty"`
	ZkPub     string       `json:"zk_pub,omitempty"`
}

type UserSig struct {
	PubKey []byte
	Sig    []byte
}

// Limits to prevent DoS via oversized inputs
const (
	maxSignatureBase58Len  = 2048
	maxSignatureDecodedLen = 4096
	maxZkProofLen          = 1024
	maxZkPubLen            = 1024
)

func (tx *Transaction) Serialize() []byte {
	amountStr := tx.Amount.String()
	metadata := fmt.Sprintf(
		"%d|%s|%s|%s|%s|%d|%s",
		tx.Type, tx.Sender, tx.Recipient, amountStr, tx.TextData, tx.Nonce, tx.ExtraInfo,
	)
	return []byte(metadata)
}

func (tx *Transaction) Verify(zkVerify *zkverify.ZkVerify) bool {
	// Validate inputs
	if tx.Signature == "" {
		logx.Error("TransactionVerify", "missing signature")
		return false
	}

	if len(tx.Signature) > maxSignatureBase58Len {
		logx.Error("TransactionVerify", "signature too large")
		return false
	}

	signature, err := common.DecodeBase58ToBytes(tx.Signature)
	if err != nil {
		logx.Error("TransactionVerify", "failed to decode signature", err)
		return false
	}

	if len(signature) > maxSignatureDecodedLen {
		logx.Error("TransactionVerify", "decoded signature too large")
		return false
	}

	serialized := tx.Serialize()

	if tx.Type == TxTypeFaucet {
		sigStr := string(signature)
		signatures := strings.Split(sigStr, "|")
		for _, sigStr := range signatures {
			sigBytes, err := common.DecodeBase58ToBytes(sigStr)
			if err != nil {
				logx.Error("TransactionVerify", "failed to decode multisig signature", err)
				return false
			}

			userSig := UserSig{}
			if err := jsonx.Unmarshal(sigBytes, &userSig); err != nil {
				logx.Error("TransactionVerify", "failed to unmarshal signature", err)
				return false
			}

			if len(userSig.PubKey) == 0 || len(userSig.Sig) == 0 {
				return false
			}

			if len(userSig.PubKey) != ed25519.PublicKeySize {
				logx.Error("TransactionVerify", "bad public key length: "+strconv.Itoa(len(userSig.PubKey)))
				return false
			}

			if !ed25519.Verify(userSig.PubKey, []byte(ADD_FAUCET_MESSAGE), userSig.Sig) {
				logx.Error("TransactionVerify", "multisig signature verification failed")
				return false
			}

		}
		return true

	} else {
		if zkVerify == nil {
			logx.Error("TransactionVerify", "zkVerify is nil for non-faucet transaction")
			return false
		}

		if tx.ZkProof == "" || tx.ZkPub == "" {
			logx.Error("TransactionVerify", "missing ZK proof or public data")
			return false
		}

		if len(tx.ZkProof) > maxZkProofLen || len(tx.ZkPub) > maxZkPubLen {
			logx.Error("TransactionVerify", "ZK fields too large")
			return false
		}

		userSig := UserSig{}
		if err := jsonx.Unmarshal(signature, &userSig); err != nil {
			logx.Error("TransactionVerify", "failed to unmarshal signature", err)
			return false
		}

		if len(userSig.PubKey) == 0 || len(userSig.Sig) == 0 {
			logx.Error("TransactionVerify", "invalid user signature structure")
			return false
		}

		if l := len(userSig.PubKey); l != ed25519.PublicKeySize {
			logx.Error("TransactionVerify", "bad public key length: "+strconv.Itoa(l))
			return false
		}

		pubKey := common.EncodeBytesToBase58(userSig.PubKey)

		ed25519Valid := ed25519.Verify(userSig.PubKey, serialized, userSig.Sig)
		if !ed25519Valid {
			logx.Error("TransactionVerify", "ED25519 verification failed")
			return false
		}

		return zkVerify.Verify(tx.Sender, pubKey, tx.ZkProof, tx.ZkPub)
	}
}

func (tx *Transaction) Bytes() []byte {
	b, _ := jsonx.Marshal(tx)
	return b
}

func (tx *Transaction) Hash() string {
	sum256 := sha256.Sum256(tx.Bytes())
	return hex.EncodeToString(sum256[:])
}
