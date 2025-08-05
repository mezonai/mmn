package service

import (
	"mezon/v2/mmn/domain"
	"mezon/v2/mmn/outbound"
)

type AccountService struct{ bc outbound.MainnetClient }

func NewAccountService(bc outbound.MainnetClient) *AccountService { return &AccountService{bc} }

func (s *AccountService) Get(addr string) (domain.Account, error) {
	account, err := s.bc.GetAccount(addr)
	if err != nil {
		return domain.Account{}, err
	}
	return account, nil
}
