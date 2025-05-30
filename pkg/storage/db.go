package storage

import "mmm/pkg/blockchain"

type DB struct {
	bc     blockchain.Blockchain
	txPool blockchain.TransactionPool
}
