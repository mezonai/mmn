package staking

type StakeType string

const (
	TypeFlex StakeType = "flex"
	TypeLock StakeType = "lock"
)

type StakeStatus string

const (
	StakePending      StakeStatus = "Pending"
	StakeActive       StakeStatus = "Active"
	StakeDeactivating StakeStatus = "Deactivating"
	StakeInactive     StakeStatus = "Inactive"
	StakeWithdrawned  StakeStatus = "Withdrawned"
	StakeExpired      StakeStatus = "Expired"
)
