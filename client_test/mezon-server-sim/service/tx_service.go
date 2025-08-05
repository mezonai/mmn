package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"mezon/v2/mmn/crypto"
	"mezon/v2/mmn/domain"
	"mezon/v2/mmn/outbound"
)

type NonceProvider interface {
	NextNonce(addr string) (uint64, error)
}

// -------- TxService --------

type TxService struct {
	bc    outbound.MainnetClient
	ks    outbound.WalletManager
	nonce NonceProvider
}

func NewTxService(bc outbound.MainnetClient, ks outbound.WalletManager, np NonceProvider) *TxService {
	return &TxService{bc: bc, ks: ks, nonce: np}
}

// SendToken forward 1 transfer token transaction to main-net.
func (s *TxService) SendToken(ctx context.Context, fromUID, toUID uint64, amount uint64, textData string) (string, error) {
	fromAddr, fromPriv, err := s.ks.LoadKey(fromUID)
	if err != nil {
		// TODO: temporary fix for integration test
		if !errors.Is(err, domain.ErrKeyNotFound) {
			fmt.Printf("SendToken LoadKey Err %d %s %s %v\n", fromUID, fromAddr, fromPriv, err)
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", fromUID)
		if fromAddr, err = s.ks.CreateKey(fromUID); err != nil {
			return "", err
		}
	}
	toAddr, _, err := s.ks.LoadKey(toUID)
	if err != nil {
		if !errors.Is(err, domain.ErrKeyNotFound) {
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", toUID)
		if toAddr, err = s.ks.CreateKey(toUID); err != nil {
			return "", err
		}
	}

	nonce, err := s.nonce.NextNonce(fromAddr)
	if err != nil {
		return "", err
	}
	unsigned, err := domain.BuildTransferTx(domain.TxTypeTransfer, fromAddr, toAddr, amount, nonce, uint64(time.Now().Unix()), textData)
	if err != nil {
		return "", err
	}

	signedRaw, err := crypto.SignTx(unsigned, fromPriv)
	if err != nil {
		return "", err
	}

	//Self verify
	if !crypto.Verify(unsigned, signedRaw.Sig) {
		return "", errors.New("self verify failed")
	}

	res, err := s.bc.AddTx(signedRaw)
	if err != nil {
		return "", err
	}

	return res.TxHash, nil
}
