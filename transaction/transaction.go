package transaction

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/logx"
	"github.com/mezonai/mmn/zkverify"
	"github.com/mr-tron/base58"
)

const (
	TransactionExtraInfoDongGiveCoffee       = "dong-give-coffee"
	TransactionExtraInfoGiveCoffee           = "give-coffee"
	TransactionExtraInfoDonationCampaign     = "donation-campaign"
	TransactionExtraInfoWithdrawCampaign     = "withdraw-campaign"
	TransactionExtraInfoLuckyMoney           = "lucky-money"
	TransactionExtraInfoTokenTransfer        = "token-transfer"
	TransactionExtraInfoDonationCampaignFeed = "donation-campaign-feed"
)

const (
	TxTypeTransferByZk  = 0
	TxTypeTransferByKey = 1
	TxTypeUserContent   = 2
)

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
	amountStr := uint256ToString(tx.Amount)
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

	if tx.Type == TxTypeTransferByKey {
		pub, err := base58ToEd25519(tx.Sender)
		if err != nil {
			logx.Error("TransactionVerify", "failed to decode sender", err)
			return false
		}
		return ed25519.Verify(pub, serialized, signature)
	} else {
		// For non-faucet transactions, require ZK verification
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

func (tx *Transaction) DedupHash() string {
	sum256 := sha256.Sum256(tx.Serialize())
	return base58.Encode(sum256[:])
}

func base58ToEd25519(addr string) (ed25519.PublicKey, error) {
	b, err := common.DecodeBase58ToBytes(addr)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid pubkey")
	}
	return ed25519.PublicKey(b), nil
}

// uint256ToString converts a *uint256.Int to string, returning "0" if nil
func uint256ToString(value *uint256.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}
