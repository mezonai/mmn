package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"mmn/client_test/mezon-server-sim/api"
	"mmn/client_test/mezon-server-sim/mmn/crypto"
	"mmn/client_test/mezon-server-sim/mmn/domain"
	"mmn/client_test/mezon-server-sim/mmn/outbound"
)

// -------- TxService --------

type TxService struct {
	bc outbound.MainnetClient
	ks outbound.WalletManager
	db *sql.DB
}

func NewTxService(bc outbound.MainnetClient, ks outbound.WalletManager, db *sql.DB) *TxService {
	return &TxService{bc: bc, ks: ks, db: db}
}

// SendToken forward 1 transfer token transaction to main-net.
func (s *TxService) SendToken(ctx context.Context, nonce uint64, fromUID, toUID uint64, amount uint64, textData string) (string, error) {
	// Validate sender, recipient exists in database
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id IN ($1, $2)", fromUID, toUID).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to validate users: %w", err)
	}
	if count != 2 {
		return "", errors.New("sender or recipient does not exist")
	}

	fromAddr, fromPriv, err := s.ks.LoadKey(fromUID)
	if err != nil {
		// TODO: temporary fix for integration test
		if !errors.Is(err, domain.ErrKeyNotFound) {
			fmt.Printf("SendToken LoadKey Err %d %s %s %v\n", fromUID, fromAddr, fromPriv, err)
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", fromUID)
		if fromAddr, fromPriv, err = s.ks.CreateKey(fromUID); err != nil {
			return "", err
		}
	}
	toAddr, _, err := s.ks.LoadKey(toUID)
	if err != nil {
		if !errors.Is(err, domain.ErrKeyNotFound) {
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", toUID)
		if toAddr, _, err = s.ks.CreateKey(toUID); err != nil {
			return "", err
		}
	}

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

func (s *TxService) ListTransactions(ctx context.Context, uid uint64, limit, page, filter int) (*api.WalletLedgerList, error) {
	addr, _, err := s.ks.LoadKey(uid)
	if err != nil {
		return nil, err
	}

	offset := (page - 1) * limit
	history, err := s.bc.GetTxHistory(addr, limit, offset, filter)
	if err != nil {
		return nil, err
	}

	txs := make([]*api.WalletLedger, len(history.Txs))
	for i, tx := range history.Txs {
		txs[i] = &api.WalletLedger{
			Id:            strconv.FormatUint(tx.Nonce, 10),
			CreateTime:    uint64(tx.Timestamp),
			UserId:        strconv.FormatUint(uid, 10),
			Value:         int32(tx.Amount),
			TransactionId: strconv.FormatUint(tx.Nonce, 10),
		}
	}

	return &api.WalletLedgerList{
		Count:        int32(history.Total),
		WalletLedger: txs,
	}, nil
}

func (s *TxService) SendTokenWithoutDatabase(ctx context.Context, nonce uint64, fromAddr, toAddr string, fromPriv []byte, amount uint64, textData string) (string, error) {
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
