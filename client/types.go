package client

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"
)

const NATIVE_DECIMAL = 6
const addressDecodedExpectedLength = 32
const TxTypeTransfer = 0

var (
	ErrInvalidAddress = errors.New("domain: invalid address format")
	ErrInvalidAmount  = errors.New("domain: amount must be > 0")
	ErrKeyNotFound    = errors.New("keystore: not found")
)

// ----- Account -----
type Account struct {
	Address string
	Balance *uint256.Int
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
type Tx struct {
	Type      int          `json:"type"`
	Sender    string       `json:"sender"`
	Recipient string       `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64       `json:"timestamp"`
	TextData  string       `json:"text_data"`
	Nonce     uint64       `json:"nonce"`
	ExtraInfo string       `json:"extra_info"`
}

func BuildTransferTx(txType int, sender, recipient string, amount *uint256.Int, nonce uint64, ts uint64, textData string, extraInfo map[string]any) (*Tx, error) {
	if err := ValidateAddress(sender); err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	if err := ValidateAddress(recipient); err != nil {
		return nil, fmt.Errorf("recipient: %w", err)
	}
	if amount == nil || amount.IsZero() {
		return nil, ErrInvalidAmount
	}

	serializedTxExtra, err := SerializeTxExtraInfo(extraInfo)
	if err != nil {
		return nil, err
	}

	return &Tx{
		Type:      txType,
		Sender:    sender,
		Recipient: recipient,
		Amount:    amount,
		Nonce:     nonce,
		Timestamp: ts,
		TextData:  textData,
		ExtraInfo: serializedTxExtra,
	}, nil
}

func SerializeTxExtraInfo(data map[string]any) (string, error) {
	if data == nil {
		return "", nil
	}

	extraBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("unable to marshal tx extra info: %w", err)
	}
	return string(extraBytes), nil
}

func DeserializeTxExtraInfo(raw string) (map[string]any, error) {
	if raw == "" {
		return nil, nil
	}

	var extraInfo map[string]any
	if err := json.Unmarshal([]byte(raw), &extraInfo); err != nil {
		return nil, fmt.Errorf("unable to unmarshal extra info: %w", err)
	}
	return extraInfo, nil
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
	Amount    *uint256.Int
	Nonce     uint64
	Timestamp uint64
	Status    TxMeta_Status
}

type TxHistoryResponse struct {
	Total uint32
	Txs   []*TxMetaResponse
}

// TxInfo represents transaction information returned by GetTxByHash
type TxInfo struct {
	Sender    string       `json:"sender"`
	Recipient string       `json:"recipient"`
	Amount    *uint256.Int `json:"amount"`
	Timestamp uint64       `json:"timestamp"`
	TextData  string       `json:"text_data"`
	Nonce     uint64       `json:"nonce,omitempty"`
	ExtraInfo string       `json:"extra_info,omitempty"`
}

func (i *TxInfo) DeserializedExtraInfo() (map[string]any, error) {
	return DeserializeTxExtraInfo(i.ExtraInfo)
}
