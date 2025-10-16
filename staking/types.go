package staking

type StakeType string

const (
	TypeFlex StakeType = "flex"
	TypeLock StakeType = "lock"
)

type StakeStatus string

const (
	StatusActive    StakeStatus = "active"
	StatusUnbonding StakeStatus = "unbonding"
	StatusUnlocked  StakeStatus = "unlocked"
	StatusWithdrawn StakeStatus = "withdrawn"
	StatusExpired   StakeStatus = "expired"
)