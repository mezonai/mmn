package interfaces

import (
	"context"

	"github.com/mezonai/mmn/transaction"
)

type Broadcaster interface {
	TxBroadcast(ctx context.Context, tx *transaction.Transaction) error
}
