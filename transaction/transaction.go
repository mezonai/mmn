package transaction

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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

func (tx *Transaction) Serialize() []byte {
	amountStr := uint256ToString(tx.Amount)
	metadata := fmt.Sprintf(
		"%d|%s|%s|%s|%s|%d|%s|%s|%s",
		tx.Type, tx.Sender, tx.Recipient, amountStr, tx.TextData, tx.Nonce, tx.ExtraInfo, tx.ZkProof, tx.ZkPub,
	)
	return []byte(metadata)
}

func (tx *Transaction) Verify(zkVerify *zkverify.ZkVerify) bool {
	pub, err := base58ToEd25519(tx.Sender)
	if err != nil {
		logx.Error("TransactionVerify", "tx.Sender", tx.Sender)
		return false
	}
	signature, err := common.DecodeBase58ToBytes(tx.Signature)
	if err != nil {
		logx.Error("TransactionVerify", "tx.Signature", tx.Signature)
		return false
	}

	if tx.Type != TxTypeFaucet {
		zkVerifyValid := zkVerify.Verify(tx.ZkProof, tx.ZkPub)
		if !zkVerifyValid {
			return false
		}
	}

	return ed25519.Verify(pub, tx.Serialize(), signature)
}

func (tx *Transaction) Bytes() []byte {
	b, _ := jsonx.Marshal(tx)
	return b
}

func (tx *Transaction) Hash() string {
	sum256 := sha256.Sum256(tx.Bytes())
	return hex.EncodeToString(sum256[:])
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
