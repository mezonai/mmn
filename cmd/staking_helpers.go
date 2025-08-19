package cmd

import (
	"fmt"
	"math/big"

	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/staking"
)

// ConvertStakingConfig converts config.StakingConfig to staking.StakeManagerConfig
// This function is placed in cmd package to avoid import cycles
func ConvertStakingConfig(stakingCfg *config.StakingConfig) (*staking.StakeManagerConfig, error) {
	minStake, ok := new(big.Int).SetString(stakingCfg.MinStakeAmount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid min_stake_amount: %s", stakingCfg.MinStakeAmount)
	}

	epochReward, ok := new(big.Int).SetString(stakingCfg.EpochRewardAmount, 10)
	if !ok {
		return nil, fmt.Errorf("invalid epoch_reward_amount: %s", stakingCfg.EpochRewardAmount)
	}

	return &staking.StakeManagerConfig{
		SlotsPerEpoch:     stakingCfg.SlotsPerEpoch,
		MinStakeAmount:    minStake,
		MaxValidators:     stakingCfg.MaxValidators,
		EpochRewardAmount: epochReward,
	}, nil
}
