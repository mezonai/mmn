package network

import (
	"context"
	"fmt"

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
