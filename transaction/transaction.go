package transaction

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/common"
)

const (
	TxTypeTransfer = 0
)

type Transaction struct {
	Type      int32       `json:"type"`
	Sender    string      `json:"sender"`
	Recipient string      `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64      `json:"timestamp"`
	TextData  string      `json:"text_data"`
	Nonce     uint64      `json:"nonce,omitempty"`
	Signature string      `json:"signature,omitempty"`
}

// Custom JSON marshaling for uint256.Int Amount field
type transactionJSON struct {
	Type      int32  `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func (tx *Transaction) MarshalJSON() ([]byte, error) {
	amountStr := "0"
	if tx.Amount != nil {
		amountStr = tx.Amount.String()
	}
	
	return json.Marshal(&transactionJSON{
		Type:      tx.Type,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    amountStr,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
		Signature: tx.Signature,
	})
}

func (tx *Transaction) UnmarshalJSON(data []byte) error {
	var aux transactionJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	tx.Type = aux.Type
	tx.Sender = aux.Sender
	tx.Recipient = aux.Recipient
	tx.Timestamp = aux.Timestamp
	tx.TextData = aux.TextData
	tx.Nonce = aux.Nonce
	tx.Signature = aux.Signature
	
	// Parse amount
	if aux.Amount == "" {
		tx.Amount = uint256.NewInt(0)
	} else {
		amount, err := uint256.FromDecimal(aux.Amount)
		if err != nil {
			return fmt.Errorf("invalid amount format: %w", err)
		}
		tx.Amount = amount
	}
	
	return nil
}

func (tx *Transaction) Serialize() []byte {
	amountStr := "0"
	if tx.Amount != nil {
		amountStr = tx.Amount.String()
	}
	metadata := fmt.Sprintf("%d|%s|%s|%s|%s|%d", tx.Type, tx.Sender, tx.Recipient, amountStr, tx.TextData, tx.Nonce)
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
