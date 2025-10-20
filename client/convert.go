package client

import (
	proto "github.com/mezonai/mmn/proto"
	"github.com/mezonai/mmn/utils"
)

func ToProtoTx(tx *Tx) *proto.TxMsg {
	return &proto.TxMsg{
		Type:      int32(tx.Type),
		Sender:    tx.Sender,
		Recipient: tx.Recipient,
		Amount:    utils.Uint256ToString(tx.Amount),
		Nonce:     tx.Nonce,
		TextData:  tx.TextData,
		Timestamp: tx.Timestamp,
		ExtraInfo: tx.ExtraInfo,
		ZkProof:   tx.ZkProof,
		ZkPub:     tx.ZkPub,
	}
}

func ToProtoSigTx(tx *SignedTx) *proto.SignedTxMsg {
	return &proto.SignedTxMsg{
		TxMsg:     ToProtoTx(tx.Tx),
		Signature: tx.Sig,
	}
}

func FromProtoAccount(acc *proto.GetAccountResponse) Account {
	return Account{
		Address: acc.Address,
		Balance: utils.Uint256FromString(acc.Balance),
		Nonce:   acc.Nonce,
	}
}
