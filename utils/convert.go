package utils

import (
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/transaction"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/poh"
	pb "github.com/mezonai/mmn/proto"
)

// -- Block --

func fromPBEntry(e *pb.Entry) poh.Entry {
	var hashArr [32]byte
	copy(hashArr[:], e.Hash)
	return poh.Entry{
		NumHashes:    e.NumHashes,
		Hash:         hashArr,
		Transactions: e.Transactions,
	}
}

func FromProtoBlock(pbBlk *pb.Block) (*block.Block, error) {
	var prev [32]byte
	if len(pbBlk.PrevHash) != 32 {
		return nil, fmt.Errorf("invalid prev_hash length")
	}
	copy(prev[:], pbBlk.PrevHash)

	entries := make([]poh.Entry, len(pbBlk.Entries))
	for i, e := range pbBlk.Entries {
		entries[i] = fromPBEntry(e)
	}

	var bh [32]byte
	if len(pbBlk.Hash) != 32 {
		return nil, fmt.Errorf("invalid block_hash length")
	}
	copy(bh[:], pbBlk.Hash)

	return &block.Block{
		Slot:      pbBlk.Slot,
		PrevHash:  prev,
		Entries:   entries,
		LeaderID:  pbBlk.LeaderId,
		Timestamp: pbBlk.Timestamp,
		Hash:      bh,
		Signature: pbBlk.Signature,
	}, nil
}

func ToProtoBlock(blk *block.Block) *pb.Block {
	return &pb.Block{
		Slot:      blk.Slot,
		PrevHash:  blk.PrevHash[:],
		Entries:   ToProtoEntries(blk.Entries),
		LeaderId:  blk.LeaderID,
		Timestamp: blk.Timestamp,
		Hash:      blk.Hash[:],
		Signature: blk.Signature,
	}
}

func ToProtoEntries(entries []poh.Entry) []*pb.Entry {
	pbEntries := make([]*pb.Entry, len(entries))
	for i, e := range entries {
		pbEntries[i] = &pb.Entry{
			NumHashes:    e.NumHashes,
			Hash:         e.Hash[:],
			Transactions: e.Transactions,
		}
	}
	return pbEntries
}

// -- Tx --

func ParseTx(data []byte) (*transaction.Transaction, error) {
	var tx transaction.Transaction
	err := json.Unmarshal(data, &tx)
	return &tx, err
}

func FromProtoSignedTx(pbTx *pb.SignedTxMsg) (*transaction.Transaction, error) {
	return &transaction.Transaction{
		Type:      pbTx.TxMsg.Type,
		Sender:    pbTx.TxMsg.Sender,
		Recipient: pbTx.TxMsg.Recipient,
		Amount:    pbTx.TxMsg.Amount,
		Timestamp: pbTx.TxMsg.Timestamp,
		TextData:  pbTx.TxMsg.TextData,
		Nonce:     pbTx.TxMsg.Nonce,
		Signature: pbTx.Signature,
	}, nil
}

func ToProtoSignedTx(tx *transaction.Transaction) *pb.SignedTxMsg {
	return &pb.SignedTxMsg{
		TxMsg:     ToProtoTx(tx),
		Signature: tx.Signature,
	}
}

func ToProtoTx(tx *transaction.Transaction) *pb.TxMsg {
	return &pb.TxMsg{
		Type:      tx.Type,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    tx.Amount,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
	}
}
