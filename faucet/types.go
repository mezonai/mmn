package faucet

import (
	"github.com/holiman/uint256"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mezonai/mmn/types"
)

var (
	CREATE_FAUCET                = "CREATE_FAUCET"
	FAUCET_ACTION                = "FAUCET_ACTION"
	ADD_SIGNATURE                = "ADD_SIGNATURE"
	REJECT_PROPOSAL              = "REJECT_PROPOSAL"
	ADD_APPROVER                 = "ADD_APPROVER"
	REMOVE_APPROVER              = "REMOVE_APPROVER"
	ADD_PROPOSER                 = "ADD_PROPOSER"
	REMOVE_PROPOSER              = "REMOVE_PROPOSER"
	EXECUTE_WHITELIST_MANAGEMENT = "EXECUTE_WHITELIST_MANAGEMENT"
	WHITELIST_MANAGEMENT_PREFIX  = "WHITELIST_MANAGEMENT:"
)

const (
	MultisigTxCreated  = "MULTISIG_TX_CREATED"
	MultisigTxUpdated  = "MULTISIG_TX_UPDATED"
	MultisigTxExecuted = "MULTISIG_TX_EXECUTED"
	MultisigTxFailed   = "MULTISIG_TX_FAILED"
	MultisigTxRejected = "MULTISIG_TX_REJECTED"

	ConfigCreated   = "CONFIG_CREATED"
	ConfigUpdated   = "CONFIG_UPDATED"
	ApproverAdded   = "APPROVER_ADDED"
	ApproverRemoved = "APPROVER_REMOVED"
	ProposerAdded   = "PROPOSER_ADDED"
	ProposerRemoved = "PROPOSER_REMOVED"
)

const (
	minThreshold     = 2
	thresholdRatio   = 2
	thresholdDivisor = 3
)

var (
	STATUS_EXECUTED = "EXECUTED"
	STATUS_PENDING  = "PENDING"
	STATUS_FAILED   = "FAILED"
	STATUS_REJECTED = "REJECTED"
)

type UserSig struct {
	PubKey []byte `json:"pub_key"`
	Sig    []byte `json:"sig"`
}

type FaucetSyncTransactionMessage struct {
	Type string           `json:"type"`
	Data types.MultisigTx `json:"data"`
}

type FaucetSyncConfigMessage struct {
	Type string               `json:"type"`
	Data types.MultisigConfig `json:"data"`
}

type FaucetSyncWhitelistMessage struct {
	Type string        `json:"type"`
	Data WhitelistData `json:"data"`
}

type MultisigTxData struct {
	Tx     *types.MultisigTx `json:"tx"`
	Action string            `json:"action"`
}

type ConfigData struct {
	Config *types.MultisigConfig `json:"config"`
	Action string                `json:"action"`
}

type WhitelistData struct {
	Address string `json:"address"`
	Type    string `json:"type"`   // "approver" or "proposer"
	Action  string `json:"action"` // "add" or "remove"
}

type RequestFaucetVoteMessage struct {
	TxHash    string   `json:"tx_hash"`
	LeaderId  string   `json:"leader_id"`
	Appovers  []string `json:"approvers"`
	Proposer  string   `json:"proposer"`
	Signature []byte   `json:"signature"`
	Approve   bool     `json:"approve"`
}

type RequestInitFaucetConfigMessage struct {
	Approvers               []string                                     `json:"approvers"`
	Proposers               []string                                     `json:"proposers"`
	MultisigTxs             []*types.MultisigTx                          `json:"multisig_txs"`
	PendingTxs              map[string]*types.MultisigTx                 `json:"pending_txs"`
	MaxAmount               *uint256.Int                                 `json:"max_amount"`
	PendingProposerRequests map[string]*types.WhitelistManagementRequest `json:"pending_proposer_requests"`
	PendingApproverRequests map[string]*types.WhitelistManagementRequest `json:"pending_approver_requests"`
	Configs                 map[string]*types.MultisigConfig             `json:"configs"`
	Votes                   map[string]map[peer.ID]bool                  `json:"votes"`
}
