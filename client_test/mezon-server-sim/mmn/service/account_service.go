package service

import (
	"context"
	"database/sql"

	mmnClient "github.com/mezonai/mmn/client"
)

type AccountService struct {
	client mmnClient.MainnetClient
	ks     mmnClient.WalletManager
	db     *sql.DB
}

func NewAccountService(client mmnClient.MainnetClient, ks mmnClient.WalletManager, db *sql.DB) *AccountService {
	return &AccountService{client: client, ks: ks, db: db}
}

func (s *AccountService) GetAccount(ctx context.Context, uid uint64) (mmnClient.Account, error) {
	addr, _, err := s.ks.LoadKey(uid)
	if err != nil {
		return mmnClient.Account{}, err
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
