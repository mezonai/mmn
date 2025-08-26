package utils

import (
	"encoding/json"
	"fmt"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/transaction"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/poh"
	pb "github.com/mezonai/mmn/proto"
)

// -- Block --

func fromPBEntry(e *pb.Entry) (poh.Entry, error) {
	var hashArr [32]byte
	copy(hashArr[:], e.Hash)
	txs := make([]*transaction.Transaction, len(e.Transactions))
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
		BlockCore: block.BlockCore{
			Slot:      b.Slot,
			Status:    block.BlockPending,
			PrevHash:  b.PrevHash,
			LeaderID:  b.LeaderID,
			Timestamp: b.Timestamp,
			Hash:      b.Hash,
			Signature: b.Signature,
		},
		Entries: entries,
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
		BlockCore: block.BlockCore{
			Slot:      pbBlk.Slot,
			PrevHash:  prev,
			LeaderID:  pbBlk.LeaderId,
			Timestamp: pbBlk.Timestamp,
			Hash:      bh,
			Signature: pbBlk.Signature,
		},
		Entries: entries,
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

func ParseTx(data []byte) (*transaction.Transaction, error) {
	var tx transaction.Transaction
	err := json.Unmarshal(data, &tx)
	return &tx, err
}

func FromProtoSignedTx(pbTx *pb.SignedTxMsg) (*transaction.Transaction, error) {
	amount := uint256.NewInt(0)
	if pbTx.TxMsg.Amount != "" {
		var err error
		amount, err = uint256.FromDecimal(pbTx.TxMsg.Amount)
		if err != nil {
			// If decimal parsing fails, try as hex
			if len(pbTx.TxMsg.Amount) >= 2 && (pbTx.TxMsg.Amount[:2] == "0x" || pbTx.TxMsg.Amount[:2] == "0X") {
				amount, err = uint256.FromHex(pbTx.TxMsg.Amount)
				if err != nil {
					// If both fail, use 0
					amount = uint256.NewInt(0)
				}
			} else {
				// If both fail, use 0
				amount = uint256.NewInt(0)
			}
		}
	}
	
	return &transaction.Transaction{
		Type:      pbTx.TxMsg.Type,
		Sender:    pbTx.TxMsg.Sender,
		Recipient: pbTx.TxMsg.Recipient,
		Amount:    amount,
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
	amount := "0"
	if tx.Amount != nil {
		amount = tx.Amount.String()
	}
	return &pb.TxMsg{
		Type:      tx.Type,
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    amount,
		Timestamp: tx.Timestamp,
		TextData:  tx.TextData,
		Nonce:     tx.Nonce,
	}
}

const (
	// DecimalScale represents the scaling factor for amounts (10^6)
	DecimalScale = 1e6
)

// GetDecimalScale returns the decimal scale factor that clients should use
func GetDecimalScale() uint64 {
	return DecimalScale
}
