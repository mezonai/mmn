package validator

import (
	"context"
	"mmn/block"
	"mmn/consensus"
)

type Broadcaster interface {
	BroadcastBlock(ctx context.Context, blk *block.Block) error
	BroadcastVote(ctx context.Context, vt *consensus.Vote) error
}
