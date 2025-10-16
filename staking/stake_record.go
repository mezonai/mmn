package staking

import (
	"time"

	"github.com/mezonai/mmn/wallet"
)

type StakeRecord struct {
	Address        [32]byte
	wallet         *wallet.Wallet
	Validator      [32]byte
	TotalStake     uint64
	CreatedAt      time.Time
	APR            uint64
	LockPeriod     time.Duration
	StartTime      time.Time
	LastClaimTime  time.Time
	UnbondingUntil time.Time
	Status         StakeStatus
}

func (stakeRecord *StakeRecord) Expired() {
	// unlock when the time wait unlock end
	// need to check correct time
	stakeRecord.Status = StatusExpired
}
func (stakeRecord *StakeRecord) canUnStake() bool {
	return stakeRecord.Status == StatusActive
}

func (stakeRecord *StakeRecord) canWithdrawn() bool {
	return stakeRecord.Status == StatusUnlocked
}

func (stakeRecord *StakeRecord) UnLocked() {
	// unlock when the time wait unlock end
	// need to check correct time
	stakeRecord.Status = StatusUnlocked
}
