package staking

import (
	"time"

	"github.com/mezonai/mmn/wallet"
)

type StakeRecord struct {
	Address          [32]byte
	wallet           *wallet.Wallet
	Validator        [32]byte
	TotalStake       uint64
	CreatedAt        time.Time
	APR              uint64
	LockPeriod       time.Duration
	ActivatedAt      time.Time
	DeactivatedAt    time.Time
	ExpiredAt        time.Time
	Status           StakeStatus
	RewardAccrued    uint64
	LastRewardUpdate time.Time
}

func (stakeRecord *StakeRecord) Expired() {
	// unlock when the time wait unlock end
	// need to check correct time
	stakeRecord.Status = StakeExpired
}

func (stakeRecord *StakeRecord) CanUnStake() bool {
	return stakeRecord.Status == StakeActive
}

func (stakeRecord *StakeRecord) CanStaking() bool {
	return stakeRecord.Status == StakePending
}

func (stakeRecord *StakeRecord) CanClaim() bool {
	return stakeRecord.Status == StakeInactive
}

func (stakeRecord *StakeRecord) UnLocked() {
	// unlock when the time wait unlock end
	// need to check correct time
	stakeRecord.Status = StakeInactive
}
