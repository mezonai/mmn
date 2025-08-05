package blockchain

import (
	"context"
	"fmt"

	"mezon/v2/mmn/domain"
	mmnpb "mezon/v2/mmn/proto"
	"mezon/v2/mmn/utils"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	cfg    MmnConfig
	conn   *grpc.ClientConn
	txCli  mmnpb.TxServiceClient
	accCli mmnpb.AccountServiceClient
}

func NewGRPCClient(cfg MmnConfig) (*GRPCClient, error) {
	conn, err := grpc.Dial(
		cfg.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // dev-net not TLS yet
		grpc.WithBlock(),
		grpc.WithTimeout(cfg.Timeout),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)
	if err != nil {
		return nil, err
	}
	return &GRPCClient{
		cfg:    cfg,
		conn:   conn,
		txCli:  mmnpb.NewTxServiceClient(conn),
		accCli: mmnpb.NewAccountServiceClient(conn),
	}, nil
}

// ---- MainnetClient interface ----

func (c *GRPCClient) AddTx(tx domain.SignedTx) (domain.AddTxResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	defer cancel()

	txMsg := utils.ToProtoSigTx(&tx)
	res, err := c.txCli.AddTx(ctx, txMsg)
	if err != nil {
		return domain.AddTxResponse{}, err
	}
	if !res.Ok {
		return domain.AddTxResponse{}, fmt.Errorf("add-tx failed: %s", res.Error)
	}
	return domain.AddTxResponse{
		Ok:     res.Ok,
		TxHash: res.TxHash,
		Error:  res.Error,
	}, nil
}

func (c *GRPCClient) GetAccount(addr string) (domain.Account, error) {
	// TODO: implement
	return domain.Account{}, nil
}

func (c *GRPCClient) GetTxHistory(addr string, limit, offset int) ([]domain.Tx, error) {
	// TODO: implement
	return nil, nil
}

func (c *GRPCClient) GetNonce(addr string) (uint64, error) {
	// TODO: implement
	return 0, nil
}
