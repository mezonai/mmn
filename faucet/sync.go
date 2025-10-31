package faucet

import (
	"github.com/mezonai/mmn/jsonx"
	"github.com/mezonai/mmn/types"
)

func CreateMultisigTxMessage(tx *types.MultisigTx, action string) FaucetSyncTransactionMessage {
	return FaucetSyncTransactionMessage{
		Type: action,
		Data: *tx,
	}
}

func CreateConfigMessage(config *types.MultisigConfig, action string) FaucetSyncConfigMessage {
	return FaucetSyncConfigMessage{
		Type: action,
		Data: *config,
	}
}

func CreateWhitelistMessage(address, whitelistType, action string) FaucetSyncWhitelistMessage {
	return FaucetSyncWhitelistMessage{
		Type: action,
		Data: WhitelistData{
			Address: address,
			Type:    whitelistType,
		},
	}
}
func (msg *FaucetSyncTransactionMessage) ToJSON() ([]byte, error) {
	return jsonx.Marshal(msg)
}

func (msg *FaucetSyncConfigMessage) ToJSON() ([]byte, error) {
	return jsonx.Marshal(msg)
}

func (msg *FaucetSyncWhitelistMessage) ToJSON() ([]byte, error) {
	return jsonx.Marshal(msg)
}

func (msg *RequestFaucetVoteMessage) ToJSON() ([]byte, error) {
	return jsonx.Marshal(msg)
}
