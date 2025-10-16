package staking

import (
	"sync"
	"time"

	"github.com/mezonai/mmn/wallet"
)


type Stake struct {
	wallet 		*wallet.Wallet
	stakedAt	time.Time
	apr			uint64
}

type StakingPool struct {
	apr 		uint64
	fee 		uint64
	stakeType 	string
	stakers		map[string]*Stake
	total		uint64

	mu         	sync.RWMutex

}

func NewStake(apr uint64, fee uint64) *StakingPool {
	return &StakingPool{
		apr: apr,
		fee: fee,
		stakers: make(map[string]*Stake),
		total: 0,
	}
}


func (stake *StakingPool) ChangeComission(newApr uint64) {
	stake.apr = newApr
}

func (stake *StakingPool) ChangeFee(fee uint64) {
	stake.fee = fee
}


func Delegate(wallet wallet.Wallet, amount uint64) {

}