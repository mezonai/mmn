package client

import (
	"errors"
	"fmt"
	"strings"

	"github.com/holiman/uint256"
	mmnpb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/utils"
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
	Type      int          `json:"type"`
	Sender    string       `json:"sender"`
	Recipient string       `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64       `json:"timestamp"`
	TextData  string       `json:"text_data"`
	Nonce     uint64       `json:"nonce"`
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
	amount := utils.Uint256ToString(tx.Amount)

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

func ToProtoSigTx(tx *SignedTx) *mmnpb.SignedTxMsg {
	return &mmnpb.SignedTxMsg{
		TxMsg:     ToProtoTx(tx.Tx),
		Signature: tx.Sig,
	}
}
