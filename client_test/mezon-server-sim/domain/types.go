package domain

import (
	"errors"
	"fmt"
)

const addressExpectedLength = 64

var (
	ErrInvalidAddress = errors.New("domain: invalid address format")
	ErrInvalidAmount  = errors.New("domain: amount must be > 0")
)

// ----- Account -----

type Account struct {
	Address string
	Balance uint64
	Nonce   uint64
}

func ValidateAddress(addr string) error {
	s := addr
	if len(s) != addressExpectedLength {
		return ErrInvalidAddress
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'f') ||
			(c >= 'A' && c <= 'F')) {
			return ErrInvalidAddress
		}
	}
	return nil
}

// ----- Tx -----

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

type SignedTx struct {
	Tx  *Tx
	Sig string
}

type AddTxResponse struct {
	Ok     bool   `json:"ok"`
	TxHash string `json:"tx_hash"`
	Error  string `json:"error"`
}
