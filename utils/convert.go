package utils

import (
	"fmt"
	"time"

	"mmn/block"
	"mmn/poh"
	pb "mmn/proto"

	"google.golang.org/protobuf/types/known/timestamppb"
)

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

	ts := time.Now()
	if pbt := pbBlk.Timestamp; pbt != nil {
		if pbt.IsValid() {
			ts = pbt.AsTime()
		}
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
		Timestamp: ts,
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
		Timestamp: timestamppb.New(blk.Timestamp),
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
