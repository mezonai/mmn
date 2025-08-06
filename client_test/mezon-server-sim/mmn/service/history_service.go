package service

import (
	"mezon/v2/mmn/domain"
	"mezon/v2/mmn/outbound"
)

type HistoryService struct{ bc outbound.MainnetClient }

func NewHistoryService(bc outbound.MainnetClient) *HistoryService { return &HistoryService{bc} }

func (s *HistoryService) List(addr string, limit, offset int) ([]domain.Tx, error) {
	return s.bc.GetTxHistory(addr, limit, offset)
}
