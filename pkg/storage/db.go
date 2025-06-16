package storage

import "mmn/pkg/blockchain"

type DB struct {
	bc     blockchain.Blockchain
	txPool blockchain.TransactionPool
}
