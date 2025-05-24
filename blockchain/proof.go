package blockchain

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"math/big"
)

const Difficulty = 10

type ProofOfWork struct {
	Block  *Block
	Target *big.Int
}

func NewProofOfWork(block *Block) *ProofOfWork {
	target := big.NewInt(1)
	target.Lsh(target, uint(256-Difficulty))
	return &ProofOfWork{block, target}
}

func (pow *ProofOfWork) ComputeData(nonce int) []byte {
	data := bytes.Join(
		[][]byte{
			[]byte(pow.Block.PrevHash),
			[]byte(pow.Block.Data),
			make([]byte, 8),
			make([]byte, 8),
		},
		[]byte{},
	)

	binary.BigEndian.PutUint64(data[len(data)-16:], uint64(nonce))
	binary.BigEndian.PutUint64(data[len(data)-8:], uint64(Difficulty))

	return data
}

func (pow *ProofOfWork) MineBlock() (int, []byte) {
	var intHash big.Int
	var computedHash [16]byte

	nonce := 0

	for {
		computedData := pow.ComputeData(nonce)
		computedHash = md5.Sum(computedData)

		fmt.Printf("\r%x", computedHash)

		intHash.SetBytes(computedHash[:])

		if intHash.Cmp(pow.Target) == -1 {
			break
		}

		nonce++
	}
	fmt.Println()

	return nonce, computedHash[:]
}

func (pow *ProofOfWork) Validate() bool {
	var intHash big.Int

	computedData := pow.ComputeData(pow.Block.Nonce)

	computedHash := md5.Sum(computedData)
	intHash.SetBytes(computedHash[:])

	if intHash.Cmp(pow.Target) == -1 {
		return true
	} else {
		return false
	}
}
