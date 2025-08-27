package client

import (
	"context"
	"fmt"

	mmnpb "github.com/mezonai/mmn/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	Endpoint string
}

type MmnClient struct {
	cfg          Config
	conn         *grpc.ClientConn
	healthClient mmnpb.HealthServiceClient
	txClient     mmnpb.TxServiceClient
	accClient    mmnpb.AccountServiceClient
}

func NewClient(cfg Config) (*MmnClient, error) {
	conn, err := grpc.NewClient(
		cfg.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	if err != nil {
		return nil, err
	}

	return &MmnClient{
		cfg:          cfg,
		conn:         conn,
		healthClient: mmnpb.NewHealthServiceClient(conn),
		txClient:     mmnpb.NewTxServiceClient(conn),
		accClient:    mmnpb.NewAccountServiceClient(conn),
	}, nil
}

func (c *MmnClient) CheckHealth(ctx context.Context) (*mmnpb.HealthCheckResponse, error) {
	health, err := c.healthClient.Check(ctx, &mmnpb.Empty{})
	if err != nil {
		return nil, err
	}

	return health, nil
}

func (c *MmnClient) AddTx(ctx context.Context, signedTx *mmnpb.SignedTxMsg) (*mmnpb.AddTxResponse, error) {
	res, err := c.txClient.AddTx(ctx, signedTx)
	if err != nil {
		return nil, err
	}
	if !res.Ok {
		return nil, fmt.Errorf("add-tx failed: %s", res.Error)
	}

	return res, nil
}

func (c *MmnClient) GetAccount(ctx context.Context, addr string) (*mmnpb.GetAccountResponse, error) {
	res, err := c.accClient.GetAccount(ctx, &mmnpb.GetAccountRequest{Address: addr})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *MmnClient) GetTxHistory(ctx context.Context, addr string, limit, offset, filter int) (*mmnpb.GetTxHistoryResponse, error) {
	res, err := c.accClient.GetTxHistory(ctx, &mmnpb.GetTxHistoryRequest{Address: addr, Limit: uint32(limit), Offset: uint32(offset), Filter: uint32(filter)})
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (c *MmnClient) Conn() *grpc.ClientConn {
	return c.conn
}

// Close closes the gRPC connection
func (c *MmnClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
