package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/holiman/uint256"
	mmnpb "github.com/mezonai/mmn/proto"
)

const addressExpectedLength = 64

var (
	ErrInvalidAddress = errors.New("domain: invalid address format")
	ErrInvalidAmount  = errors.New("domain: amount must be > 0")
)

const (
	TxTypeTransfer = 0
)

type Tx struct {
	Type      int         `json:"type"`
	Sender    string      `json:"sender"`
	Recipient string      `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64      `json:"timestamp"`
	TextData  string      `json:"text_data"`
	Nonce     uint64      `json:"nonce"`
}

// Custom JSON marshaling for uint256.Int Amount field
type txJSON struct {
	Type      int    `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    string `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce"`
}

func (tx *Tx) MarshalJSON() ([]byte, error) {
	amountStr := "0"
	if tx.Amount != nil {
		amountStr = tx.Amount.String()
	}
	
	return json.Marshal(&txJSON{
		Type:      tx.Type,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    amountStr,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
	})
}

func (tx *Tx) UnmarshalJSON(data []byte) error {
	var aux txJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	tx.Type = aux.Type
	tx.Sender = aux.Sender
	tx.Recipient = aux.Recipient
	tx.Timestamp = aux.Timestamp
	tx.TextData = aux.TextData
	tx.Nonce = aux.Nonce
	
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

type SignedTx struct {
	Tx  *Tx
	Sig string
}

func ValidateAddress(addr string) error {
	s := addr
	if len(s) != addressExpectedLength {
		return fmt.Errorf("%w: expected length %d, got %d", ErrInvalidAddress, addressExpectedLength, len(s))
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'f') ||
			(c >= 'A' && c <= 'F')) {
			return fmt.Errorf("%w: invalid character '%c' at position %d", ErrInvalidAddress, c, strings.Index(s, string(c)))
		}
	}
	return nil
}

func BuildTransferTx(txType int, sender, recipient string, amount *uint256.Int, nonce uint64, ts uint64, textData string) (*Tx, error) {
	if err := ValidateAddress(sender); err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	if err := ValidateAddress(recipient); err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	if amount == nil || amount.IsZero() {
		return nil, ErrInvalidAmount
	}

	return &Tx{
		Type:      txType,
		Sender:    sender,
		Recipient: recipient,
		Amount:    amount,
		Nonce:     nonce,
		Timestamp: ts,
		TextData:  textData,
	}, nil
}

func ToProtoTx(tx *Tx) *mmnpb.TxMsg {
	amount := "0"
	if tx.Amount != nil {
		amount = tx.Amount.String()
	}
	return &mmnpb.TxMsg{
		Type:      int32(tx.Type),
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    amount,
		Nonce:     tx.Nonce,
		TextData:  tx.TextData,
		Timestamp: tx.Timestamp,
	}
}

func FromProtoTx(protoTx *mmnpb.TxMsg) *Tx {
	amount := uint256.NewInt(0)
	if protoTx.Amount != "" {
		var err error
		amount, err = uint256.FromDecimal(protoTx.Amount)
		if err != nil {
			// If decimal parsing fails, try as hex
			if len(protoTx.Amount) >= 2 && (protoTx.Amount[:2] == "0x" || protoTx.Amount[:2] == "0X") {
				amount, err = uint256.FromHex(protoTx.Amount)
				if err != nil {
					// If both fail, use 0
					amount = uint256.NewInt(0)
				}
			} else {
				// If both fail, use 0
				amount = uint256.NewInt(0)
			}
		}
	}
	
	return &Tx{
		Type:      int(protoTx.Type),
		Sender:    protoTx.Sender,
		Recipient: protoTx.Recipient,
		Amount:    amount,
		Nonce:     protoTx.Nonce,
		TextData:  protoTx.TextData,
		Timestamp: protoTx.Timestamp,
	}
}

func ToProtoSigTx(tx *SignedTx) *mmnpb.SignedTxMsg {
	return &mmnpb.SignedTxMsg{
		TxMsg:     ToProtoTx(tx.Tx),
		Signature: tx.Sig,
	}
}
