package utils

import (
	"encoding/json"
	"fmt"

	"github.com/mezonai/mmn/types"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/poh"
	pb "github.com/mezonai/mmn/proto"
)

// -- Block --

func fromPBEntry(e *pb.Entry) (poh.Entry, error) {
	var hashArr [32]byte
	copy(hashArr[:], e.Hash)
	txs := make([]*types.Transaction, len(e.Transactions))
	for i, txBytes := range e.Transactions {
		tx, err := ParseTx(txBytes)
		if err != nil {
			return poh.Entry{}, err
		}
		txs[i] = tx
	}
	return poh.Entry{
		NumHashes:    e.NumHashes,
		Hash:         hashArr,
		Transactions: txs,
	}, nil
}

func BroadcastedBlockToBlock(b *block.BroadcastedBlock) *block.Block {
	entries := make([]poh.PersistentEntry, len(b.Entries))
	for i, entry := range b.Entries {
		txHashes := make([]string, len(entry.Transactions))
		for i, tx := range entry.Transactions {
			txHashes[i] = tx.Hash()
		}
		entries[i] = poh.PersistentEntry{
			NumHashes: entry.NumHashes,
			Hash:      entry.Hash,
			TxHashes:  txHashes,
			Tick:      entry.Tick,
		}
	}

	blk := &block.Block{
		Slot:      b.Slot,
		Status:    block.BlockPending,
		PrevHash:  b.PrevHash,
		Entries:   entries,
		LeaderID:  b.LeaderID,
		Timestamp: b.Timestamp,
		Hash:      b.Hash,
		Signature: b.Signature,
	}

	return blk
}

func FromProtoBlock(pbBlk *pb.Block) (*block.BroadcastedBlock, error) {
	var prev [32]byte
	if len(pbBlk.PrevHash) != 32 {
		return nil, fmt.Errorf("invalid prev_hash length")
	}
	copy(prev[:], pbBlk.PrevHash)

	entries := make([]poh.Entry, len(pbBlk.Entries))
	for i, e := range pbBlk.Entries {
		entry, err := fromPBEntry(e)
		if err != nil {
			return nil, err
		}
		entries[i] = entry
	}

	var bh [32]byte
	if len(pbBlk.Hash) != 32 {
		return nil, fmt.Errorf("invalid block_hash length")
	}
	copy(bh[:], pbBlk.Hash)

	return &block.BroadcastedBlock{
		Slot:      pbBlk.Slot,
		PrevHash:  prev,
		Entries:   entries,
		LeaderID:  pbBlk.LeaderId,
		Timestamp: pbBlk.Timestamp,
		Hash:      bh,
		Signature: pbBlk.Signature,
	}, nil
}

func ToProtoBlock(blk *block.BroadcastedBlock) (*pb.Block, error) {
	entries, err := ToProtoEntries(blk.Entries)
	if err != nil {
		return nil, err
	}
	return &pb.Block{
		Slot:      blk.Slot,
		PrevHash:  blk.PrevHash[:],
		Entries:   entries,
		LeaderId:  blk.LeaderID,
		Timestamp: blk.Timestamp,
		Hash:      blk.Hash[:],
		Signature: blk.Signature,
	}, nil
}

func ToProtoEntries(entries []poh.Entry) ([]*pb.Entry, error) {
	pbEntries := make([]*pb.Entry, len(entries))
	for i, e := range entries {
		txs := make([][]byte, len(e.Transactions))
		for j, tx := range e.Transactions {
			txBytes, err := json.Marshal(tx)
			if err != nil {
				return nil, err
			}
			txs[j] = txBytes
		}
		pbEntries[i] = &pb.Entry{
			NumHashes:    e.NumHashes,
			Hash:         e.Hash[:],
			Transactions: txs,
		}
	}
	return pbEntries, nil
}

// -- Tx --

func ParseTx(data []byte) (*types.Transaction, error) {
	var tx types.Transaction
	err := json.Unmarshal(data, &tx)
	return &tx, err
}

func FromProtoSignedTx(pbTx *pb.SignedTxMsg) (*types.Transaction, error) {
	return &types.Transaction{
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

func ToProtoSignedTx(tx *types.Transaction) *pb.SignedTxMsg {
	return &pb.SignedTxMsg{
		TxMsg:     ToProtoTx(tx),
		Signature: tx.Signature,
	}
}

func ToProtoTx(tx *types.Transaction) *pb.TxMsg {
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
