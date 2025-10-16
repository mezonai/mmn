package staking

import (
	"sync"
	"time"

	"github.com/mezonai/mmn/wallet"
)



type Validator struct {
	Address		[32]byte
	owner		*wallet.Wallet
	APR 		uint64
	fee 		uint64
	stakeType 	StakeType
	TotalStake	uint64

	mu         	sync.RWMutex
	CreatedAt   time.Time

}

func NewStake(APR uint64, fee uint64, stakeType string, owner *wallet.Wallet) *Validator {
	return &Validator{
		APR: 			APR,
		fee: 			fee,
		TotalStake: 	0,
		stakeType: 		TypeFlex,
		owner: 			owner,
		CreatedAt:		time.Now(),
	}
}

func (validator *Validator) ChangeComission(newAPR uint64) {
	validator.APR = newAPR
}

func (validator *Validator) ChangeFee(fee uint64) {
	validator.fee = fee
}


func (validator *Validator) Delegate(wallet *wallet.Wallet, amount uint64) *StakeRecord {
	validator.mu.Lock()
	defer validator.mu.Unlock()
	validator.TotalStake	+=	amount
	return &StakeRecord{
		wallet: wallet,
		CreatedAt: time.Now(),
		APR: validator.APR,
		Validator: validator.Address,
		StartTime: time.Now(),
		Status: StatusActive,
		TotalStake: amount,
	}
}

// request unstake (wait to claim)
func (validator *Validator) Unstake(StakeRecord *StakeRecord,wallet *wallet.Wallet) {
	validator.mu.Lock()
	defer validator.mu.Unlock()

	if StakeRecord.canUnStake() {
		StakeRecord.Status = StatusUnbonding
		validator.TotalStake -=	StakeRecord.TotalStake
		// calculator and update the UnbondingUntil
	}

}

func (validator *Validator) Withdrawn(StakeRecord *StakeRecord,wallet *wallet.Wallet) {
	validator.mu.Lock()
	defer validator.mu.Unlock()

	if StakeRecord.canWithdrawn() {
		StakeRecord.LastClaimTime = time.Now()
		StakeRecord.Status = StatusWithdrawn
		// TODO: transfer token to staker = totalStake + (totalStake * APR%) - fee
	}

}

func (validator *Validator) CalculatorUnstakeAvailableDay() time.Time {
	return time.Now()
}

func (validator *Validator) CalculatorCurrentAPR() uint64 {
	return 0
}

func calculatorAPR() uint64 {
	return 0
}

