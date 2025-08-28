package blockchain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mezonai/mmn/client_test/mezon-server-sim/mezoncfg"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/domain"
	mmnpb "github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/proto"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/utils"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
	manual "google.golang.org/grpc/resolver/manual"
)

type GRPCClient struct {
	cfg    mezoncfg.MmnConfig
	conn   *grpc.ClientConn
	txCli  mmnpb.TxServiceClient
	accCli mmnpb.AccountServiceClient
}

func NewGRPCClient(cfg mezoncfg.MmnConfig) (*GRPCClient, error) {
	var conn *grpc.ClientConn
	var err error

	// Check if endpoint contains multiple addresses (comma-separated)
	if strings.Contains(cfg.Endpoints, ",") {
		// Parse comma-separated endpoints
		endpoints := strings.Split(cfg.Endpoints, ",")
		addresses := make([]resolver.Address, 0, len(endpoints))

		for _, endpoint := range endpoints {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint != "" {
				addresses = append(addresses, resolver.Address{Addr: endpoint})
			}
		}

		if len(addresses) == 0 {
			return nil, fmt.Errorf("no valid endpoints found in: %s", cfg.Endpoints)
		}

		// Create manual resolver for multiple endpoints
		r := manual.NewBuilderWithScheme("mmn")
		r.InitialState(resolver.State{
			Addresses: addresses,
		})

		conn, err = grpc.NewClient(
			"mmn:///",
			grpc.WithResolvers(r),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		)
	} else {
		// Single endpoint - use original logic
		conn, err = grpc.NewClient(
			cfg.Endpoints,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`),
		)
	}

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

func (c *GRPCClient) SubscribeTransactionStatus(ctx context.Context) (mmnpb.TxService_SubscribeTransactionStatusClient, error) {
	stream, err := c.txCli.SubscribeTransactionStatus(ctx, &mmnpb.SubscribeTransactionStatusRequest{})
	if err != nil {
		fmt.Printf("SubscribeTransactionStatus error: %v", err)
		return nil, err
	}
	return stream, nil
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
