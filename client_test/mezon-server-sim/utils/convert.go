package utils

import (
	"mezon/v2/mmn/domain"
	proto "mezon/v2/mmn/proto"
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
