package types

import (
	"encoding/json"
	"fmt"

	"github.com/holiman/uint256"
)

type TxRecord struct {
	Slot      uint64
	Amount    *uint256.Int
	Sender    string
	Recipient string
	Timestamp uint64
	TextData  string
	Type      int32
	Nonce     uint64
}

type Account struct {
	Address string
	Balance *uint256.Int
	Nonce   uint64
	History []string // tx hashes
}

type SnapshotAccount struct {
	Balance *uint256.Int
	Nonce   uint64
}

// Custom JSON marshaling for uint256.Int fields
type accountJSON struct {
	Address string   `json:"address"`
	Balance string   `json:"balance"`
	Nonce   uint64   `json:"nonce"`
	History []string `json:"history"`
}

func (a *Account) MarshalJSON() ([]byte, error) {
	balanceStr := "0"
	if a.Balance != nil {
		balanceStr = a.Balance.String()
	}
	
	return json.Marshal(&accountJSON{
		Address: a.Address,
		Balance: balanceStr,
		Nonce:   a.Nonce,
		History: a.History,
	})
}

func (a *Account) UnmarshalJSON(data []byte) error {
	var aux accountJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	a.Address = aux.Address
	a.Nonce = aux.Nonce
	a.History = aux.History
	
	// Parse balance
	if aux.Balance == "" {
		a.Balance = uint256.NewInt(0)
	} else {
		balance, err := uint256.FromDecimal(aux.Balance)
		if err != nil {
			return fmt.Errorf("invalid balance format: %w", err)
		}
		a.Balance = balance
	}
	
	return nil
}

// Custom JSON marshaling for SnapshotAccount
type snapshotAccountJSON struct {
	Balance string `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

func (sa *SnapshotAccount) MarshalJSON() ([]byte, error) {
	balanceStr := "0"
	if sa.Balance != nil {
		balanceStr = sa.Balance.String()
	}
	
	return json.Marshal(&snapshotAccountJSON{
		Balance: balanceStr,
		Nonce:   sa.Nonce,
	})
}

func (sa *SnapshotAccount) UnmarshalJSON(data []byte) error {
	var aux snapshotAccountJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	
	sa.Nonce = aux.Nonce
	
	// Parse balance
	if aux.Balance == "" {
		sa.Balance = uint256.NewInt(0)
	} else {
		balance, err := uint256.FromDecimal(aux.Balance)
		if err != nil {
			return fmt.Errorf("invalid balance format: %w", err)
		}
		sa.Balance = balance
	}
	
	return nil
}
