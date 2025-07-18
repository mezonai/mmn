package ledger

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	TxTypeTransfer = 0
	TxTypeFaucet   = 1
)

type Transaction struct {
	Type      int    `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    uint64 `json:"amount"`
	Timestamp int64  `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func (tx *Transaction) Serialize() []byte {
	//TODO: buffer from dummy string 1234567890
	buf := bytes.NewBuffer([]byte("1234567890"))
	return buf.Bytes()
}

func (tx *Transaction) Verify() bool {
	pub, err := hexToEd25519(tx.Sender)
	if err != nil {
		return false
	}
	signature, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, tx.Serialize(), signature)
}

func (tx *Transaction) Bytes() []byte {
	b, _ := json.Marshal(tx)
	return b
}

func hexToEd25519(hexstr string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(hexstr)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid pubkey")
	}
	return ed25519.PublicKey(b), nil
}

func ParseTx(data []byte) (*Transaction, error) {
	var tx Transaction
	err := json.Unmarshal(data, &tx)
	return &tx, err
}
