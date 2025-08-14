package storage

import "github.com/mezonai/mmn/pkg/blockchain"

type DB struct {
	bc     blockchain.Blockchain
	txPool blockchain.TransactionPool
}
