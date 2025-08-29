package utils

import (
	"github.com/holiman/uint256"
	mmnClient "github.com/mezonai/mmn/client"
)

func ToDecimal(s *uint256.Int) int64 {
	scale := new(uint256.Int).Exp(uint256.NewInt(10), uint256.NewInt(mmnClient.NATIVE_DECIMAL))
	return int64(new(uint256.Int).Div(s, scale).Uint64())
}

func ToBigNumber(s int64) *uint256.Int {
	scale := new(uint256.Int).Exp(uint256.NewInt(10), uint256.NewInt(mmnClient.NATIVE_DECIMAL))
	return new(uint256.Int).Mul(uint256.NewInt(uint64(s)), scale)
}
