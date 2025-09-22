package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mmnClient "github.com/mezonai/mmn/client"
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/keystore"
)

type AccountService struct {
	client mmnClient.MainnetClient
	ks     keystore.WalletManager
	db     *sql.DB
}

func NewAccountService(client mmnClient.MainnetClient, ks keystore.WalletManager, db *sql.DB) *AccountService {
	return &AccountService{client: client, ks: ks, db: db}
}

func (s *AccountService) GetAccount(ctx context.Context, uid uint64) (mmnClient.Account, error) {
	addr, _, err := s.ks.LoadKey(uid)
	if err != nil {
		if !errors.Is(err, mmnClient.ErrKeyNotFound) {
			fmt.Printf("SendToken LoadKey Err %d %s %s\n", uid, addr, err)
			return mmnClient.Account{}, err
		}
		fmt.Printf("SendToken CreateKey %d\n", uid)
		if addr, _, err = s.ks.CreateKey(uid); err != nil {
			return mmnClient.Account{}, err
		}
	}
	account, err := s.client.GetAccount(ctx, addr)
	if err != nil {
		return mmnClient.Account{}, err
	}

	return account, nil
}

func (s *AccountService) GetAccountByAddress(ctx context.Context, addr string) (mmnClient.Account, error) {
	account, err := s.client.GetAccount(ctx, addr)
	if err != nil {
		return mmnClient.Account{}, err
	}

	return account, nil
}
