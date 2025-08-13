package client

import (
	"errors"
	"fmt"
	"strings"

	mmnpb "github.com/mezonai/mmn/proto"
)

const addressExpectedLength = 64

var (
	ErrInvalidAddress = errors.New("domain: invalid address format")
	ErrInvalidAmount  = errors.New("domain: amount must be > 0")
)

const (
	TxTypeTransfer = 0
	TxTypeFaucet   = 1
)

type Tx struct {
	Type      int    `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    uint64 `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce"`
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

func BuildTransferTx(txType int, sender, recipient string, amount uint64, nonce uint64, ts uint64, textData string) (*Tx, error) {
	if err := ValidateAddress(sender); err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	if err := ValidateAddress(recipient); err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	if amount == 0 {
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
	return &mmnpb.TxMsg{
		Type:      int32(tx.Type),
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    tx.Amount,
		Nonce:     tx.Nonce,
		TextData:  tx.TextData,
		Timestamp: tx.Timestamp,
	}
}

func ToProtoSigTx(tx *SignedTx) *mmnpb.SignedTxMsg {
	return &mmnpb.SignedTxMsg{
		TxMsg:     ToProtoTx(tx.Tx),
		Signature: tx.Sig,
	}
}
