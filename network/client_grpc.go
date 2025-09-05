package network

import (
	"context"
	"fmt"
	"github.com/mezonai/mmn/logx"
	"time"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/consensus"
	pb "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/utils"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	peers []string
	opts  []grpc.DialOption
}

func NewGRPCClient(peers []string) *GRPCClient {
	return &GRPCClient{
		peers: peers,
		opts: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	}
}

func (c *GRPCClient) BroadcastBlock(ctx context.Context, blk *block.BroadcastedBlock) error {
	// TODO: should reuse connection instead of creating a new one for each peer
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			fmt.Printf("[gRPC Client] NewClient %s failed: %v", addr, err)
			continue
		}
		client := pb.NewBlockServiceClient(conn)
		rpcCtx, cancel := context.WithTimeout(ctx, time.Second)
		fmt.Printf("Broadcasting block %d to %s\n", blk.Slot, addr)
		pbBlk, err := utils.ToProtoBlock(blk)
		if err != nil {
			fmt.Printf("[gRPC Client] ToProtoBlock error: %v", err)
			continue
		}
		resp, err := client.Broadcast(rpcCtx, pbBlk)
		cancel()
		if err != nil {
			fmt.Printf("[gRPC Client] Broadcast to %s error: %v", addr, err)
		} else if !resp.Ok {
			fmt.Printf("[gRPC Client] Broadcast to %s not OK: %s", addr, resp.Error)
		}
		fmt.Printf("Broadcasted block %d to %s successfully\n", blk.Slot, addr)
		conn.Close()
	}
	return nil
}

func (c *GRPCClient) BroadcastVote(ctx context.Context, vt *consensus.Vote) error {
	pbV := &pb.VoteRequest{
		Slot:      vt.Slot,
		BlockHash: vt.BlockHash[:],
		VoterId:   vt.VoterID,
		Signature: vt.Signature,
	}
	// TODO: should reuse connection instead of creating a new one for each peer
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			continue
		}
		client := pb.NewVoteServiceClient(conn)
		fmt.Printf("Broadcasting vote %d to %s\n", vt.Slot, addr)
		resp, err := client.Vote(ctx, pbV)
		if err != nil {
			fmt.Printf("[gRPC Client] Vote to %s error: %v", addr, err)
		} else if !resp.Ok {
			fmt.Printf("[gRPC Client] Vote to %s not OK: %s", addr, resp.Error)
		}
		fmt.Printf("Broadcasted vote %d to %s successfully\n", vt.Slot, addr)
		conn.Close()
	}
	return nil
}

func (c *GRPCClient) TxBroadcast(ctx context.Context, tx *transaction.Transaction) error {
	pbTx := utils.ToProtoSignedTx(tx)
	// TODO: should reuse connection instead of creating a new one for each peer
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			continue
		}
		client := pb.NewTxServiceClient(conn)
		fmt.Printf("Broadcasting tx %+v to %s\n", tx, addr)
		resp, err := client.TxBroadcast(ctx, pbTx)
		if err != nil {
			fmt.Printf("[gRPC Client] TxBroadcast to %s error: %v", addr, err)
		} else if !resp.Ok {
			fmt.Printf("[gRPC Client] TxBroadcast to %s not OK: %s", addr, resp.Error)
		}
		fmt.Printf("Broadcasted tx %+v to %s successfully\n", tx, addr)
		conn.Close()
	}
	return nil
}
