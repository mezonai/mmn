package faucet

import (
	"time"

	"github.com/holiman/uint256"
)

type ProposalStatus string

const (
	ProposalPending           ProposalStatus = "PENDING"
	ProposalPartiallyApproved ProposalStatus = "PARTIALLY_APPROVED"
	ProposalFullyApproved     ProposalStatus = "FULLY_APPROVED"
	ProposalBroadcasted       ProposalStatus = "BROADCASTED"
	ProposalRejected          ProposalStatus = "REJECTED"
)

type Proposal struct {
	ID        string            `json:"id"`
	Sender    string            `json:"sender"`
	Recipient string            `json:"recipient"`
	Amount    string            `json:"amount"` // decimal string
	Message   string            `json:"message"`
	Nonce     uint64            `json:"nonce"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	Threshold int               `json:"threshold"`
	Signers   []string          `json:"signers"`
	Approvals map[string]string `json:"approvals"` // signer -> note/signature
	Status    ProposalStatus    `json:"status"`
}

func (p *Proposal) ApprovedCount() int {
	return len(p.Approvals)
}

func (p *Proposal) IsFullyApproved() bool {
	return p.ApprovedCount() >= p.Threshold
}

func (p *Proposal) AmountAsUint256() (*uint256.Int, error) {
	return uint256.FromDecimal(p.Amount)
}
