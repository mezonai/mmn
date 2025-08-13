package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"mmn/client_test/mezon-server-sim/api"
	"mmn/client_test/mezon-server-sim/mmn/crypto"
	"mmn/client_test/mezon-server-sim/mmn/domain"
	"mmn/client_test/mezon-server-sim/mmn/outbound"
	mmnpb "mmn/client_test/mezon-server-sim/mmn/proto"
)

// -------- TxService --------

type TxService struct {
	bc outbound.MainnetClient
	ks outbound.WalletManager
	db *sql.DB
}

func NewTxService(bc outbound.MainnetClient, ks outbound.WalletManager, db *sql.DB) *TxService {
	return &TxService{bc: bc, ks: ks, db: db}
}

// SendToken forward 1 transfer token transaction to main-net.
func (s *TxService) SendToken(ctx context.Context, nonce uint64, fromUID, toUID uint64, amount uint64, textData string) (string, error) {
	// Validate sender, recipient exists in database
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id IN ($1, $2)", fromUID, toUID).Scan(&count)
	if err != nil {
		return "", fmt.Errorf("failed to validate users: %w", err)
	}
	if count != 2 {
		return "", errors.New("sender or recipient does not exist")
	}

	fromAddr, fromPriv, err := s.ks.LoadKey(fromUID)
	if err != nil {
		// TODO: temporary fix for integration test
		if !errors.Is(err, domain.ErrKeyNotFound) {
			fmt.Printf("SendToken LoadKey Err %d %s %s %v\n", fromUID, fromAddr, fromPriv, err)
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", fromUID)
		if fromAddr, fromPriv, err = s.ks.CreateKey(fromUID); err != nil {
			return "", err
		}
	}
	toAddr, _, err := s.ks.LoadKey(toUID)
	if err != nil {
		if !errors.Is(err, domain.ErrKeyNotFound) {
			return "", err
		}
		fmt.Printf("SendToken CreateKey %d\n", toUID)
		if toAddr, _, err = s.ks.CreateKey(toUID); err != nil {
			return "", err
		}
	}

	if err != nil {
		return "", err
	}
	unsigned, err := domain.BuildTransferTx(domain.TxTypeTransfer, fromAddr, toAddr, amount, nonce, uint64(time.Now().Unix()), textData)
	if err != nil {
		return "", err
	}

	signedRaw, err := crypto.SignTx(unsigned, fromPriv)
	if err != nil {
		return "", err
	}

	//Self verify
	if !crypto.Verify(unsigned, signedRaw.Sig) {
		return "", errors.New("self verify failed")
	}

	res, err := s.bc.AddTx(signedRaw)
	if err != nil {
		return "", err
	}

	return res.TxHash, nil
}

// GetAccountByAddress gets account information by address
func (s *TxService) GetAccountByAddress(ctx context.Context, addr string) (domain.Account, error) {
	return s.bc.GetAccount(addr)
}

func (s *TxService) ListTransactions(ctx context.Context, uid uint64, limit, page, filter int) (*api.WalletLedgerList, error) {
	addr, _, err := s.ks.LoadKey(uid)
	if err != nil {
		return nil, err
	}

	offset := (page - 1) * limit
	history, err := s.bc.GetTxHistory(addr, limit, offset, filter)
	if err != nil {
		return nil, err
	}

	txs := make([]*api.WalletLedger, len(history.Txs))
	for i, tx := range history.Txs {
		txs[i] = &api.WalletLedger{
			Id:            strconv.FormatUint(tx.Nonce, 10),
			CreateTime:    uint64(tx.Timestamp),
			UserId:        strconv.FormatUint(uid, 10),
			Value:         int32(tx.Amount),
			TransactionId: strconv.FormatUint(tx.Nonce, 10),
		}
	}

	return &api.WalletLedgerList{
		Count:        int32(history.Total),
		WalletLedger: txs,
	}, nil
}

func (s *TxService) SendTokenWithoutDatabase(ctx context.Context, nonce uint64, fromAddr, toAddr string, fromPriv []byte, amount uint64, textData string, transferType int) (string, error) {
	unsigned, err := domain.BuildTransferTx(transferType, fromAddr, toAddr, amount, nonce, uint64(time.Now().Unix()), textData)
	if err != nil {
		return "", err
	}

	signedRaw, err := crypto.SignTx(unsigned, fromPriv)
	if err != nil {
		return "", err
	}

	//Self verify
	if !crypto.Verify(unsigned, signedRaw.Sig) {
		return "", errors.New("self verify failed")
	}

	res, err := s.bc.AddTx(signedRaw)
	if err != nil {
		return "", err
	}

	return res.TxHash, nil
}

func (s *TxService) ListFaucetTransactions(ctx context.Context, limit, page, filter int) (*api.WalletLedgerList, error) {
	offset := (page - 1) * limit
	addr := "0d1dfad29c20c13dccff213f52d2f98a395a0224b5159628d2bdb077cf4026a7"
	history, err := s.bc.GetTxHistory(addr, limit, offset, filter)
	if err != nil {
		return nil, err
	}

	txs := make([]*api.WalletLedger, len(history.Txs))
	for i, tx := range history.Txs {
		txs[i] = &api.WalletLedger{
			Id:            strconv.FormatUint(tx.Nonce, 10),
			CreateTime:    uint64(tx.Timestamp),
			UserId:        "faucet",
			Value:         int32(tx.Amount),
			TransactionId: strconv.FormatUint(tx.Nonce, 10),
		}
	}

	return &api.WalletLedgerList{
		Count:        int32(history.Total),
		WalletLedger: txs,
	}, nil
}

// SubscribeTransactionStatus subscribes to transaction status updates from the MMN server
// and processes them with custom business logic including database updates
func (s *TxService) SubscribeTransactionStatus(ctx context.Context) error {
	// Subscribe to transaction status updates
	stream, err := s.bc.SubscribeTransactionStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe to transaction status: %w", err)
	}

	log.Printf("Successfully subscribed to transaction status updates")

	// Process incoming status updates
	for {
		select {
		case <-ctx.Done():
			log.Printf("Transaction status subscription context cancelled")
			return ctx.Err()
		default:
			// Receive status update from stream
			update, err := stream.Recv()
			if err != nil {
				log.Printf("Error receiving transaction status update: %v", err)
				return fmt.Errorf("stream error: %w", err)
			}

			// Process the transaction status update
			if err := s.processTransactionStatusUpdate(ctx, update); err != nil {
				log.Printf("Error processing transaction status update for tx %s: %v",
					update.TxHash, err)
				// Continue processing other updates even if one fails
				continue
			}

			log.Printf("Successfully processed status update for tx %s: %s",
				update.TxHash, update.Status.String())
		}
	}
}

// processTransactionStatusUpdate handles the business logic for each transaction status update
func (s *TxService) processTransactionStatusUpdate(ctx context.Context, update *mmnpb.TransactionStatusUpdate) error {
	// Example business logic based on transaction status
	switch update.Status {
	case mmnpb.TransactionStatus_PENDING:
		// Handle pending transactions
		log.Printf("Transaction %s is pending", update.TxHash)
		return s.updateTransactionStatusInDB(ctx, update.TxHash, "PENDING", update.Timestamp)

	case mmnpb.TransactionStatus_CONFIRMED:
		// Handle confirmed transactions
		log.Printf("Transaction %s confirmed in block %s at slot %d",
			update.TxHash, update.BlockHash, update.BlockSlot)

		if err := s.updateTransactionStatusInDB(ctx, update.TxHash, "CONFIRMED", update.Timestamp); err != nil {
			return err
		}

		// Additional logic for confirmed transactions
		return s.handleConfirmedTransaction(ctx, update)

	case mmnpb.TransactionStatus_FINALIZED:
		// Handle finalized transactions
		log.Printf("Transaction %s finalized with %d confirmations",
			update.TxHash, update.Confirmations)

		if err := s.updateTransactionStatusInDB(ctx, update.TxHash, "FINALIZED", update.Timestamp); err != nil {
			return err
		}

		// Additional logic for finalized transactions
		return s.handleFinalizedTransaction(ctx, update)

	case mmnpb.TransactionStatus_FAILED:
		// Handle failed transactions
		log.Printf("Transaction %s failed: %s", update.TxHash, update.ErrorMessage)

		if err := s.updateTransactionStatusInDB(ctx, update.TxHash, "FAILED", update.Timestamp); err != nil {
			return err
		}

		// Additional logic for failed transactions
		return s.handleFailedTransaction(ctx, update)

	case mmnpb.TransactionStatus_EXPIRED:
		// Handle expired transactions
		log.Printf("Transaction %s expired", update.TxHash)
		return s.updateTransactionStatusInDB(ctx, update.TxHash, "EXPIRED", update.Timestamp)

	default:
		log.Printf("Unknown transaction status for tx %s: %s", update.TxHash, update.Status.String())
	}

	return nil
}

// updateTransactionStatusInDB updates transaction status in the database
func (s *TxService) updateTransactionStatusInDB(ctx context.Context, txHash, status string, timestamp uint64) error {
	query := `
		INSERT INTO transaction_status (tx_hash, status, timestamp, updated_at) 
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tx_hash) 
		DO UPDATE SET 
			status = EXCLUDED.status, 
			timestamp = EXCLUDED.timestamp,
			updated_at = EXCLUDED.updated_at
	`

	_, err := s.db.ExecContext(ctx, query, txHash, status, timestamp, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("failed to update transaction status in database: %w", err)
	}

	return nil
}

// handleConfirmedTransaction processes business logic for confirmed transactions
func (s *TxService) handleConfirmedTransaction(ctx context.Context, update *mmnpb.TransactionStatusUpdate) error {
	// Example: Update user balances, send notifications, etc.
	log.Printf("Processing confirmed transaction %s in block %s", update.TxHash, update.BlockHash)

	// Add custom business logic here, such as:
	// - Updating user balances
	// - Sending push notifications
	// - Triggering webhooks
	// - Updating analytics data

	return nil
}

// handleFinalizedTransaction processes business logic for finalized transactions
func (s *TxService) handleFinalizedTransaction(ctx context.Context, update *mmnpb.TransactionStatusUpdate) error {
	// Example: Final settlement, compliance reporting, etc.
	log.Printf("Processing finalized transaction %s with %d confirmations",
		update.TxHash, update.Confirmations)

	// Add custom business logic here, such as:
	// - Final settlement processing
	// - Compliance reporting
	// - Archiving transaction data
	// - Updating final account states

	return nil
}

// handleFailedTransaction processes business logic for failed transactions
func (s *TxService) handleFailedTransaction(ctx context.Context, update *mmnpb.TransactionStatusUpdate) error {
	// Example: Refund processing, error notifications, etc.
	log.Printf("Processing failed transaction %s: %s", update.TxHash, update.ErrorMessage)

	// Add custom business logic here, such as:
	// - Processing refunds
	// - Sending error notifications to users
	// - Logging failed transaction details
	// - Rolling back related operations

	return nil
}
