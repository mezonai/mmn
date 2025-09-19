package transaction

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/common"
	"github.com/mezonai/mmn/jsonx"
	pb "github.com/mezonai/mmn/proto"
	"google.golang.org/protobuf/proto"
)

const (
	TxTypeTransfer = 0
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
}

func (tx *Transaction) Serialize() []byte {
	amountStr := uint256ToString(tx.Amount)
	metadata := fmt.Sprintf(
		"%d|%s|%s|%s|%s|%d|%s",
		tx.Type, tx.Sender, tx.Recipient, amountStr, tx.TextData, tx.Nonce, tx.ExtraInfo,
	)
	return []byte(metadata)
}

func (tx *Transaction) Verify() bool {
	pub, err := base58ToEd25519(tx.Sender)
	if err != nil {
		return false
	}
	signature, err := common.DecodeBase58ToBytes(tx.Signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, tx.Serialize(), signature)
}

func (tx *Transaction) Bytes() []byte {
	b, _ := jsonx.Marshal(tx)
	return b
}

func (tx *Transaction) Hash() string {
	// Hash protobuf-encoded TxMsg for compact, stable representation
	msg := &pb.TxMsg{
		Type:      tx.Type,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    uint256ToString(tx.Amount),
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
		ExtraInfo: tx.ExtraInfo,
	}
	b, _ := proto.Marshal(msg)
	sum256 := sha256.Sum256(b)
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
