package interfaces

import (
	"context"
	"mmn/block"
	"mmn/consensus"
	"mmn/types"
)

type Broadcaster interface {
	BroadcastBlock(ctx context.Context, blk *block.Block) error
	BroadcastVote(ctx context.Context, vt *consensus.Vote) error
	TxBroadcast(ctx context.Context, tx *types.Transaction) error
}
