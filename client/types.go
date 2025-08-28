package client

import (
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

const addressDecodedExpectedLength = 32

var (
	ErrInvalidAddress = errors.New("domain: invalid address format")
	ErrInvalidAmount  = errors.New("domain: amount must be > 0")
	ErrKeyNotFound    = errors.New("keystore: not found")
)

// ----- Account -----
type Account struct {
	Address string
	Balance uint64
	Nonce   uint64
}

func ValidateAddress(addr string) error {
	decoded, err := base58.Decode(addr)
	if err != nil {
		return ErrInvalidAddress
	}
	if len(decoded) != addressDecodedExpectedLength {
		return ErrInvalidAddress
	}
	return nil
}

// ----- Tx -----
const (
	TxTypeTransfer = 0
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

type TxMeta_Status int32

const (
	TxMeta_Status_PENDING   TxMeta_Status = 0
	TxMeta_Status_CONFIRMED TxMeta_Status = 1
	TxMeta_Status_FINALIZED TxMeta_Status = 2
	TxMeta_Status_FAILED    TxMeta_Status = 3
)

type TxMetaResponse struct {
	Sender    string
	Recipient string
	Amount    uint64
	Nonce     uint64
	Timestamp uint64
	Status    TxMeta_Status
}

type TxHistoryResponse struct {
	Total uint32
	Txs   []*TxMetaResponse
}

const (
	UNLOCK_ITEM_STATUS_PENDING = 0
	UNLOCK_ITEM_STATUS_SUCCESS = 1
	UNLOCK_ITEM_STATUS_FAILED  = 2
)

// TxInfo represents transaction information returned by GetTxByHash
type TxInfo struct {
	Type      int32  `json:"type"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Amount    uint64 `json:"amount"`
	Timestamp uint64 `json:"timestamp"`
	TextData  string `json:"text_data"`
	Nonce     uint64 `json:"nonce,omitempty"`
	Signature string `json:"signature,omitempty"`
}
