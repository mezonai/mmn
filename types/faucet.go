package types

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/holiman/uint256"
	"github.com/mr-tron/base58"
)

type MultisigConfig struct {
	Threshold int      `json:"threshold"`
	Signers   []string `json:"signers"`
	Address   string   `json:"address"`
}

type MultisigSignature struct {
	Signer    string `json:"signer"`
	Signature string `json:"signature"`
	ZkProof   string `json:"zk_proof,omitempty"`
	ZkPub     string `json:"zk_pub,omitempty"`
}

type MultisigTx struct {
	Type       int                 `json:"type"`
	Sender     string              `json:"sender"`
	Recipient  string              `json:"recipient"`
	Amount     *uint256.Int        `json:"amount"`
	Timestamp  uint64              `json:"timestamp"`
	TextData   string              `json:"text_data"`
	Nonce      uint64              `json:"nonce"`
	ExtraInfo  string              `json:"extra_info"`
	Signatures []MultisigSignature `json:"signatures"`
	Config     MultisigConfig      `json:"config"`
	Status     string              `json:"status"` // pending, executed, failed
}

func (tx *MultisigTx) Hash() string {
	hashData := struct {
		Type        int    `json:"type"`
		Sender      string `json:"sender"`
		Recipient   string `json:"recipient"`
		Amount      string `json:"amount"`
		Timestamp   uint64 `json:"timestamp"`
		TextData    string `json:"text_data"`
		Nonce       uint64 `json:"nonce"`
		ExtraInfo   string `json:"extra_info"`
		Threshold   int    `json:"threshold"`
		SignerCount int    `json:"signer_count"`
	}{
		Type:        tx.Type,
		Sender:      tx.Sender,
		Recipient:   tx.Recipient,
		Amount:      tx.Amount.String(),
		Timestamp:   tx.Timestamp,
		TextData:    tx.TextData,
		Nonce:       tx.Nonce,
		ExtraInfo:   tx.ExtraInfo,
		Threshold:   tx.Config.Threshold,
		SignerCount: len(tx.Config.Signers),
	}

	jsonData, _ := json.Marshal(hashData)
	hash := sha256.Sum256(jsonData)
	return base58.Encode(hash[:])
}

type TransactionStatus struct {
	TxHash         string              `json:"tx_hash"`
	IsComplete     bool                `json:"is_complete"`
	SignatureCount int                 `json:"signature_count"`
	RequiredCount  int                 `json:"required_count"`
	Recipient      string              `json:"recipient"`
	Amount         *uint256.Int        `json:"amount"`
	CreatedAt      time.Time           `json:"created_at"`
	Signatures     []MultisigSignature `json:"signatures"`
}

type ServiceStats struct {
	RegisteredConfigs   int           `json:"registered_configs"`
	PendingTransactions int           `json:"pending_transactions"`
	MaxAmount           *uint256.Int  `json:"max_amount"`
	Cooldown            time.Duration `json:"cooldown"`
}

type WhitelistManagementRequest struct {
	Action     string            `json:"action"`      // ADD_APPROVER, REMOVE_APPROVER, ADD_PROPOSER, REMOVE_PROPOSER
	TargetAddr string            `json:"target_addr"` // add/remove
	Signatures map[string]string `json:"signatures"`  // signer_address -> signature
	CreatedAt  time.Time         `json:"created_at"`
}
