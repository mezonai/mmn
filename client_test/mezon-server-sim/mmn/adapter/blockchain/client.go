package blockchain

import (
	"context"
	"fmt"
	"mmn/client_test/mezon-server-sim/mezoncfg"
	"mmn/client_test/mezon-server-sim/mmn/domain"
	mmnpb "mmn/client_test/mezon-server-sim/mmn/proto"
	"mmn/client_test/mezon-server-sim/mmn/utils"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GRPCClient struct {
	cfg    mezoncfg.MmnConfig
	conn   *grpc.ClientConn
	txCli  mmnpb.TxServiceClient
	accCli mmnpb.AccountServiceClient
}

func NewGRPCClient(cfg mezoncfg.MmnConfig) (*GRPCClient, error) {
	conn, err := grpc.Dial(
		cfg.Endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // dev-net not TLS yet
		grpc.WithBlock(),
		grpc.WithTimeout(time.Duration(cfg.Timeout)*time.Millisecond),
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.Timeout)*time.Millisecond)
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.Timeout)*time.Millisecond)
	defer cancel()

	res, err := c.accCli.GetAccount(ctx, &mmnpb.GetAccountRequest{Address: addr})
	if err != nil {
		return domain.Account{Address: addr, Balance: 0, Nonce: 0}, err
	}
	return utils.FromProtoAccount(res), nil
}

func (c *GRPCClient) GetTxHistory(addr string, limit, offset, filter int) (domain.TxHistoryResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.Timeout)*time.Millisecond)
	defer cancel()

	res, err := c.accCli.GetTxHistory(ctx, &mmnpb.GetTxHistoryRequest{Address: addr, Limit: uint32(limit), Offset: uint32(offset), Filter: uint32(filter)})
	if err != nil {
		return domain.TxHistoryResponse{}, err
	}
	return utils.FromProtoTxHistory(res), nil
}

func (c *GRPCClient) GetTxByHash(txHash string) (domain.TxInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.cfg.Timeout)*time.Millisecond)
	defer cancel()

	res, err := c.txCli.GetTxByHash(ctx, &mmnpb.GetTxByHashRequest{TxHash: txHash})
	if err != nil {
		return domain.TxInfo{}, err
	}
	if res.Error != "" {
		return domain.TxInfo{}, fmt.Errorf("get-tx-by-hash failed: %s", res.Error)
	}

	return domain.TxInfo{
		Sender:    res.Tx.Sender,
		Recipient: res.Tx.Recipient,
		Amount:    res.Tx.Amount,
		Timestamp: res.Tx.Timestamp,
		TextData:  res.Tx.TextData,
	}, nil
}
