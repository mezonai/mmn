package service

import (
	"context"
	"testing"
	"time"

	mmnpb "github.com/mezonai/mmn/client_test/mezon-server-sim/mmn/proto"
)

// Test processTransactionStatusInfo function with different status types
func TestTxService_processTransactionStatusInfo_StatusHandling(t *testing.T) {
	// Create a minimal TxService for testing (without database for simplicity)
	service := &TxService{}

	ctx := context.Background()

	// Test different transaction statuses to ensure they don't panic
	testCases := []struct {
		name   string
		update *mmnpb.TransactionStatusInfo
	}{
		{
			name: "PENDING status",
			update: &mmnpb.TransactionStatusInfo{
				TxHash:    "tx_pending",
				Status:    mmnpb.TransactionStatus_PENDING,
				Timestamp: uint64(time.Now().Unix()),
			},
		},
		{
			name: "CONFIRMED status",
			update: &mmnpb.TransactionStatusInfo{
				TxHash:    "tx_confirmed",
				Status:    mmnpb.TransactionStatus_CONFIRMED,
				BlockHash: "block123",
				BlockSlot: 100,
				Timestamp: uint64(time.Now().Unix()),
			},
		},
		{
			name: "FAILED status",
			update: &mmnpb.TransactionStatusInfo{
				TxHash:       "tx_failed",
				Status:       mmnpb.TransactionStatus_FAILED,
				ErrorMessage: "Insufficient balance",
				Timestamp:    uint64(time.Now().Unix()),
			},
		},
		{
			name: "FINALIZED status",
			update: &mmnpb.TransactionStatusInfo{
				TxHash:        "tx_finalized",
				Status:        mmnpb.TransactionStatus_FINALIZED,
				Confirmations: 10,
				Timestamp:     uint64(time.Now().Unix()),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This test mainly ensures that processTransactionStatusInfo
			// doesn't panic for different status types and handles them gracefully
			// Note: This will result in database errors since we don't have a real DB,
			// but that's expected for this unit test
			err := service.processTransactionStatusInfo(ctx, tc.update)

			// We expect database errors since no DB is connected, but no panics
			if err == nil {
				t.Logf("Unexpected success for tx %s (probably missing database connection)", tc.update.TxHash)
			} else {
				t.Logf("Expected error for tx %s: %v", tc.update.TxHash, err)
			}
		})
	}
}

// Test helper function to convert protobuf status to string
func TestGetStatusString(t *testing.T) {
	testCases := []struct {
		status   mmnpb.TransactionStatus
		expected string
	}{
		{mmnpb.TransactionStatus_PENDING, "PENDING"},
		{mmnpb.TransactionStatus_CONFIRMED, "CONFIRMED"},
		{mmnpb.TransactionStatus_FINALIZED, "FINALIZED"},
		{mmnpb.TransactionStatus_FAILED, "FAILED"},
	}

	for _, tc := range testCases {
		result := getStatusString(tc.status)
		if result != tc.expected {
			t.Errorf("Expected %s for status %v, got %s", tc.expected, tc.status, result)
		}
	}
}

// Helper function to convert protobuf status to string
func getStatusString(status mmnpb.TransactionStatus) string {
	switch status {
	case mmnpb.TransactionStatus_PENDING:
		return "PENDING"
	case mmnpb.TransactionStatus_CONFIRMED:
		return "CONFIRMED"
	case mmnpb.TransactionStatus_FINALIZED:
		return "FINALIZED"
	case mmnpb.TransactionStatus_FAILED:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}
