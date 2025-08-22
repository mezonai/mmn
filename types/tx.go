package types

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/pkg/common"
)

const (
	TxTypeTransfer = 0
)

type Transaction struct {
	Type      int32  `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    uint64 `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func (tx *Transaction) Serialize() []byte {
	metadata := fmt.Sprintf("%d|%s|%s|%d|%s|%d", tx.Type, tx.Sender, tx.Recipient, tx.Amount, tx.TextData, tx.Nonce)
	fmt.Println("Serialize metadata:", metadata)
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
	b, _ := json.Marshal(tx)
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
