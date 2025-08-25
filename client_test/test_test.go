package bench

import (
	"math/big"
	"testing"
)

func BenchmarkUint64Add(b *testing.B) {
	var x, y, z uint64 = 1<<63 - 1, 123456789, 0
	for i := 0; i < b.N; i++ {
		z = x + y
	}
	_ = z
}

func BenchmarkBigIntAdd(b *testing.B) {
	x := new(big.Int).SetUint64(1<<63 - 1)
	y := new(big.Int).SetUint64(123456789)
	z := new(big.Int)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		z.Add(x, y)
	}
}

func BenchmarkUint64Mul(b *testing.B) {
	var x, y, z uint64 = 1<<63 - 1, 123456789, 0
	for i := 0; i < b.N; i++ {
		z = x * y
	}
	_ = z
}

func BenchmarkBigIntMul(b *testing.B) {
	x := new(big.Int).SetUint64(1<<63 - 1)
	y := new(big.Int).SetUint64(123456789)
	z := new(big.Int)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		z.Mul(x, y)
	}
}