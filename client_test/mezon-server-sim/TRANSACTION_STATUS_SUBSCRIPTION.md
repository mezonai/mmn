# Transaction Status Subscription Example

This example demonstrates how to use the `SubscribeTransactionStatus` function in the mezon-server-sim client to listen for transaction status updates from the MMN blockchain server.

## Features

The `SubscribeTransactionStatus` function provides:

1. **Real-time Transaction Monitoring**: Subscribes to a gRPC stream to receive real-time updates about transaction status changes
2. **Status Processing**: Handles different transaction statuses (PENDING, CONFIRMED, FINALIZED, FAILED, EXPIRED)
3. **Database Integration**: Automatically updates transaction status in the database
4. **Custom Business Logic**: Extensible handlers for each transaction status

## Transaction Status Flow

1. **PENDING**: Transaction is in mempool waiting to be processed
2. **CONFIRMED**: Transaction is included in a block
3. **FINALIZED**: Transaction is finalized with enough confirmations
4. **FAILED**: Transaction failed validation
5. **EXPIRED**: Transaction expired due to timeout

## Usage

### 1. Basic Setup

```go
// Create transaction service
txService := service.NewTxService(mainnetClient, walletManager, db)

// Subscribe to transaction status updates
ctx := context.Background()
err := txService.SubscribeTransactionStatus(ctx)
if err != nil {
    log.Fatalf("Failed to subscribe: %v", err)
}
```

### 2. Running the Example

```bash
# Set environment variables (optional)
export MMN_ENDPOINT="localhost:9001"
export DATABASE_URL="postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"
export MASTER_KEY="bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA="

# Run the example
go run example_tx_status_subscriber.go
```

### 3. Database Schema

The function automatically creates a `transaction_status` table:

```sql
CREATE TABLE transaction_status (
    id SERIAL PRIMARY KEY,
    tx_hash VARCHAR(64) UNIQUE NOT NULL,
    status VARCHAR(20) NOT NULL,
    timestamp BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

## Architecture

### Components

1. **TxService.SubscribeTransactionStatus()**: Main subscription function
2. **processTransactionStatusUpdate()**: Processes each status update
3. **updateTransactionStatusInDB()**: Updates database with new status
4. **handleConfirmedTransaction()**: Custom logic for confirmed transactions
5. **handleFinalizedTransaction()**: Custom logic for finalized transactions
6. **handleFailedTransaction()**: Custom logic for failed transactions

### Data Flow

```
MMN Server → gRPC Stream → SubscribeTransactionStatus() → processTransactionStatusUpdate() → Database + Custom Logic
```

## Customization

### Adding Custom Business Logic

Extend the handler functions in `tx_service.go`:

```go
func (s *TxService) handleConfirmedTransaction(ctx context.Context, update *mmnpb.TransactionStatusUpdate) error {
    // Add your custom logic here:
    // - Update user balances
    // - Send push notifications
    // - Trigger webhooks
    // - Update analytics data
    
    return nil
}
```

### Error Handling

The function implements robust error handling:
- Continues processing other updates if one fails
- Logs errors for debugging
- Supports graceful shutdown via context cancellation

### Production Considerations

1. **Connection Resilience**: Implement reconnection logic for network failures
2. **Batch Processing**: Consider batching database updates for better performance
3. **Monitoring**: Add metrics and monitoring for subscription health
4. **Scaling**: Use multiple instances with proper load balancing

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MMN_ENDPOINT` | `localhost:9001` | MMN server gRPC endpoint |
| `DATABASE_URL` | `postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable` | PostgreSQL connection string |
| `MASTER_KEY` | `bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=` | Base64 encoded master key for encryption |

## Integration with Existing Code

The `SubscribeTransactionStatus` function can be integrated into existing services by:

1. Starting it as a background goroutine
2. Using context for graceful shutdown
3. Sharing the same database connection
4. Leveraging existing logging and monitoring infrastructure

Example integration:

```go
// In your main service
go func() {
    if err := txService.SubscribeTransactionStatus(ctx); err != nil {
        log.Printf("Transaction subscription error: %v", err)
    }
}()
```
