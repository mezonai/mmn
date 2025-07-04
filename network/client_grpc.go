package network

import (
	"context"
	"log"
	"time"

	"mmn/consensus"
	pb "mmn/proto"

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

func (c *GRPCClient) BroadcastBlock(ctx context.Context, blk *pb.Block) error {
	for _, addr := range c.peers {
		conn, err := grpc.NewClient(addr, c.opts...)
		if err != nil {
			log.Printf("[gRPC Client] NewClient %s failed: %v", addr, err)
			continue
		}
		client := pb.NewBlockServiceClient(conn)
		rpcCtx, cancel := context.WithTimeout(ctx, time.Second)
		resp, err := client.Broadcast(rpcCtx, blk)
		cancel()
		if err != nil {
			log.Printf("[gRPC Client] Broadcast to %s error: %v", addr, err)
		} else if !resp.Ok {
			log.Printf("[gRPC Client] Broadcast to %s not OK: %s", addr, resp.Error)
		}
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
		_, _ = client.Vote(ctx, pbV)
		conn.Close()
	}
	return nil
}
