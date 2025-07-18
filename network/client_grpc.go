package network

import (
	"context"
	"fmt"
	"time"

	"mmn/block"
	"mmn/consensus"
	pb "mmn/proto"
	"mmn/utils"

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

func (c *GRPCClient) BroadcastBlock(ctx context.Context, blk *block.Block) error {
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			fmt.Printf("[gRPC Client] NewClient %s failed: %v", addr, err)
			continue
		}
		client := pb.NewBlockServiceClient(conn)
		rpcCtx, cancel := context.WithTimeout(ctx, time.Second)
		resp, err := client.Broadcast(rpcCtx, utils.ToProtoBlock(blk))
		cancel()
		if err != nil {
			fmt.Printf("[gRPC Client] Broadcast to %s error: %v", addr, err)
		} else if !resp.Ok {
			fmt.Printf("[gRPC Client] Broadcast to %s not OK: %s", addr, resp.Error)
		}
		fmt.Printf("Broadcasted block %d to %s\n", blk.Slot, addr)
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
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			continue
		}
		client := pb.NewVoteServiceClient(conn)
		resp, err := client.Vote(ctx, pbV)
		if err != nil {
			fmt.Printf("[gRPC Client] Vote to %s error: %v", addr, err)
		} else if !resp.Ok {
			fmt.Printf("[gRPC Client] Vote to %s not OK: %s", addr, resp.Error)
		}
		fmt.Printf("Broadcasted vote %d to %s\n", vt.Slot, addr)
		conn.Close()
	}
	return nil
}
