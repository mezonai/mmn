package blockchain

import (
	"mezon/v2/mmn/service"
)

type rpcNonce struct{ cli *GRPCClient }

func (n rpcNonce) NextNonce(addr string) (uint64, error) {
	// TODO: need handle pending transaction nonce
	nonce, err := n.cli.GetNonce(addr)
	if err != nil {
		return 0, err
	}
	return nonce + 1, nil
}

func NewNonceProvider(cli *GRPCClient) service.NonceProvider {
	return rpcNonce{cli: cli}
}
