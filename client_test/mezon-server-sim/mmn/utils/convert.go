package utils

import (
	"github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/domain"
	proto "github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/proto"
)

func ToProtoTx(tx *domain.Tx) *proto.TxMsg {
	return &proto.TxMsg{
		Type:      int32(tx.Type),
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    tx.Amount,
		Nonce:     tx.Nonce,
		TextData:  tx.TextData,
		Timestamp: tx.Timestamp,
	}
}

func ToProtoSigTx(tx *domain.SignedTx) *proto.SignedTxMsg {
	return &proto.SignedTxMsg{
		TxMsg:     ToProtoTx(tx.Tx),
		Signature: tx.Sig,
	}
}

func FromProtoAccount(acc *proto.GetAccountResponse) domain.Account {
	return domain.Account{
		Address: acc.Address,
		Balance: acc.Balance,
		Nonce:   acc.Nonce,
	}
}

func FromProtoTxHistory(res *proto.GetTxHistoryResponse) domain.TxHistoryResponse {
	txs := make([]*domain.TxMetaResponse, len(res.Txs))
	for i, tx := range res.Txs {
		txs[i] = &domain.TxMetaResponse{
			Sender:    tx.Sender,
			Recipient: tx.Recipient,
			Amount:    tx.Amount,
			Nonce:     tx.Nonce,
			Timestamp: tx.Timestamp,
			Status:    domain.TxMeta_Status(tx.Status),
		}
	}
	return domain.TxHistoryResponse{
		Total: res.Total,
		Txs:   txs,
	}
}
