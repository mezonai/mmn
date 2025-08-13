package interfaces

import (
	"context"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"
	"github.com/mezonai/mmn/types"
)

type Broadcaster interface {
	BroadcastBlock(ctx context.Context, blk *block.Block) error
	BroadcastVote(ctx context.Context, vt *consensus.Vote) error
	TxBroadcast(ctx context.Context, tx *types.Transaction) error
}
