package utils

import (
	"math/big"
	"net"
)

func BigIntToFloat64(i *big.Int, decimals int) float64 {
	scale := new(big.Float).SetFloat64(float64(1))
	for j := 0; j < decimals; j++ {
		scale.Mul(scale, big.NewFloat(10))
	}

	f := new(big.Float).SetInt(i)
	result := new(big.Float).Quo(f, scale)

	f64, _ := result.Float64()
	return f64
}

func GetFreePort() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.Port, nil
}
