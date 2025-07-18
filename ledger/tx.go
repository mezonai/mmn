package ledger

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type Transaction struct {
	From      string
	To        string
	Amount    uint64
	Nonce     uint64
	Signature []byte
}

func (tx *Transaction) Serialize() []byte {
	data, _ := json.Marshal(struct {
		From   string
		To     string
		Amount uint64
		Nonce  uint64
	}{
		From: tx.From, To: tx.To, Amount: tx.Amount, Nonce: tx.Nonce,
	})
	return data
}

func (tx *Transaction) Verify() bool {
	pub, err := hexToEd25519(tx.From)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, tx.Serialize(), tx.Signature)
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
