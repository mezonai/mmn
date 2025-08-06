package service

import (
	"database/sql"
	"mezon/v2/mmn/domain"
	"mezon/v2/mmn/outbound"
)

type AccountService struct {
	bc outbound.MainnetClient
	ks outbound.WalletManager
	db *sql.DB
}

func NewAccountService(bc outbound.MainnetClient, ks outbound.WalletManager, db *sql.DB) *AccountService {
	return &AccountService{bc: bc, ks: ks, db: db}
}

func (s *AccountService) GetAccount(uid uint64) (domain.Account, error) {
	addr, _, err := s.ks.LoadKey(uid)
	if err != nil {
		return domain.Account{}, err
	}

	account, err := s.bc.GetAccount(addr)
	if err != nil {
		return domain.Account{}, err
	}
	return account, nil
}
