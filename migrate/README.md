# MMN Blockchain Migration Tool

This tool migrates user data from the current database to MMN blockchain by creating wallets and transferring tokens from faucet.

## Features
- ✅ **Automatic wallet creation**: Create Ed25519 wallets for all users in database
- ✅ **Successful token transfer**: Transfer tokens from faucet account to user wallets with signature errors fixed
- ✅ **AES-GCM private key encryption**: Store AES-GCM encrypted private keys in database (upgraded from hex encoding)
- ✅ **Connection checking**: Automatically check database and blockchain connections
- ✅ **Automatic nonce management**: Manage nonce automatically to avoid transaction conflicts
- ✅ **Detailed structured logging**: Track migration process with colored structured logging and timestamps
- ✅ **Modular architecture**: Code organized into separate modules (wallet, database, transfer, logger)
- ✅ **Upsert operations**: Automatically update existing wallets gracefully
- ✅ **Genesis faucet integration**: Use correct faucet account from genesis config
- ✅ **Transaction type compatibility**: Use FaucetTxType for funding transactions

## System Requirements

- Go 1.19+
- PostgreSQL database
- Running MMN blockchain node
- gRPC connection to MMN node

## Configuration

### Database
```sql
-- Users table (existing)
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    wallet INTEGER NOT NULL
);

-- mmn_user_keys table (will be created automatically)
CREATE TABLE mmn_user_keys (
    user_id BIGINT PRIMARY KEY,
    address VARCHAR(255) NOT NULL,
    enc_privkey BYTEA NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

### Environment Variables
```bash
# Database connection
export DATABASE_URL="postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"

# MMN blockchain endpoint
export MMN_ENDPOINT="localhost:9002"

# Master key for encryption (base64)
export MASTER_KEY="bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA="
```

## Usage

### 1. Run basic migration
```bash
cd migrate
go run .
```

### 2. Run with custom parameters
```bash
cd migrate
go run . \
  -endpoint="localhost:9002" \
  -db="postgres://user:pass@localhost:5432/db?sslmode=disable" \
  -master-key="your_base64_master_key"
```

### 3. Dry run (no actual changes)
```bash
cd migrate
go run . -dry-run=true
```

## Command Line Options

| Option | Description | Default Value |
|--------|-------------|---------------|
| `-endpoint` | MMN blockchain gRPC endpoint | `localhost:9002` |
| `-db` | Database connection URL | `postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable` |
| `-master-key` | Master key for encryption (base64) | `bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=` |
| `-dry-run` | Run migration without making actual changes | `false` |

**Note**: Other configurations are loaded from environment variables:
- `DATABASE_URL`: Database connection string
- `MMN_ENDPOINT`: MMN blockchain gRPC endpoint
- `MASTER_KEY`: Master key for encryption (base64)

## Migration Process

1. **Connection check**
   - Check database connection
   - Check MMN blockchain gRPC connection
   - Create `mmn_user_keys` table if not exists

2. **Get user list**
   - Query users from `users` table
   - Check if user already has wallet

3. **Create wallets**
   - Generate Ed25519 key pair
   - Encrypt private key with master key (AES-GCM)
   - Save to database with upsert operation
   - Automatically update `updated_at` timestamp

4. **Transfer tokens**
   - Get faucet account info from genesis config
   - Build and sign transaction with Ed25519
   - Send transaction via gRPC
   - 2-second delay between transactions
   - Handle broadcast transaction errors gracefully

## Utility Scripts

### Check database
```bash
psql "$DATABASE_URL" -c "SELECT COUNT(*) FROM users;"
psql "$DATABASE_URL" -c "SELECT COUNT(*) FROM mmn_user_keys;"
```

### Clean wallets (for testing)
```bash
# Delete all wallets in database
psql "$DATABASE_URL" -c "DELETE FROM mmn_user_keys;"

# Or drop and recreate table
psql "$DATABASE_URL" -c "DROP TABLE IF EXISTS mmn_user_keys;"
```

## File Structure

```
migrate/
├── migrate.go          # Main entry point and orchestration
├── wallet.go           # Wallet creation and management
├── database.go         # Database operations and schema management
├── transfer.go         # Token transfer and transaction handling
├── logger.go           # Structured logging system with colors
├── config.go           # Configuration and constants
├── README.md           # This documentation
└── go.mod              # Go module dependencies
```

## Module Architecture

### migrate.go
- `main()`: Main entry point, orchestrate entire migration process
- `parseLogLevel()`: Convert string log level to LogLevel enum
- Handle command line flags and initialize logger

### wallet.go
- `GetFaucetAccount()`: Get faucet account from genesis private key (fixed)
- `NewPgEncryptedStore()`: Create wallet manager with AES-GCM encryption
- `LoadKey()`: Load private key from database and decrypt
- `CreateKey()`: Create new Ed25519 key pair and encrypt
- `encrypt()/decrypt()`: Encrypt/decrypt private key with AES-GCM

### database.go
- `ConnectDatabase()`: Connect to PostgreSQL with retry mechanism
- `CreateUserKeysTable()`: Create/recreate mmn_user_keys table
- `GetUsers()`: Get list of users to migrate
- `CheckExistingWallet()`: Check if wallet already exists
- `CountExistingWallets()`: Count existing wallets

### transfer.go
- `TransferTokens()`: Execute token transfer from faucet to user
- `defaultClient()`: Create MMN client to communicate with blockchain
- Use Ed25519 signature and FaucetTxType

### logger.go
- `InitLogger()`: Initialize global logger with log level
- `LogDebug/Info/Warn/Error/Fatal()`: Logging functions with colors
- `LogMigrationStart/Complete()`: Specialized logs for migration
- `LogUserProcessing/WalletCreated/TokenTransfer()`: Detailed process logs
- `LogConnectionTest()`: Connection check logs
- Support colors and timestamps for each log level

### types.go
- `Wallet`: Struct containing wallet information
- `Tx`: Transaction structure
- `SignedTx`: Signed transaction structure

### config.go
- `LoadConfig()`: Load configuration from environment variables
- `getEnv()`: Helper function to get env with default values
- Default configuration constants

## Security Notes

- ⚠️ **Master key**: Do not commit master key to git
- ✅ **Private keys**: AES-GCM encrypted before saving to database
- ⚠️ **Database credentials**: Use environment variables
- ✅ **Faucet private key**: Use correct genesis faucet key from config
- ⚠️ **Upsert operations**: Automatically update existing wallets
- ✅ **Transaction signing**: Properly implemented Ed25519 signature scheme
- ✅ **Structured logging**: Logs do not contain sensitive information like private keys

## Logs

The tool outputs the following information with **structured logging with colors and timestamps**:

### Log types:
- 🟢 **INFO**: Important information (green)
- 🔵 **DEBUG**: Debug details (blue) 
- 🟡 **WARN**: Warnings (yellow)
- 🔴 **ERROR**: Errors (red)
- 🟣 **FATAL**: Critical errors (purple)

### Information logged:
- ✅ Successful database and blockchain connections
- 📊 Number of users to migrate
- 🔑 Wallet addresses created for each user
- 💰 Successful token transfers
- 📈 Faucet balance and address tracking
- 🎯 Migration success rate
- ⏱️ Migration completion time

### Example successful output:
```
[2024-01-15 10:30:15] INFO 🚀 Starting MMN Migration Tool (dry-run: false, log-level: info)
[2024-01-15 10:30:15] INFO 📋 Configuration loaded - MMN Endpoint: localhost:9002
[2024-01-15 10:30:16] INFO ✅ Database connection established successfully
[2024-01-15 10:30:16] INFO ✅ mmn_user_keys table ready
[2024-01-15 10:30:16] INFO 📊 Found 0 existing wallets
[2024-01-15 10:30:16] INFO ✅ Faucet account ready - Address: 0d1dfad29c20c13dccff213f52d2f98a395a0224b5159628d2bdb077cf4026a7
[2024-01-15 10:30:17] INFO 💰 Wallet created for user 1 - Address: 8373dee5a8b4c5e6f7890123456789abcdef0123
[2024-01-15 10:30:17] INFO 💸 Token transfer: 0d1dfad... → 8373dee... (amount: 1000)
[2024-01-15 10:30:19] INFO 💰 Wallet created for user 2 - Address: 9484eff6b9c5d6f7a901234567890abcdef01234
[2024-01-15 10:30:19] INFO 💸 Token transfer: 0d1dfad... → 9484eff... (amount: 1500)
[2024-01-15 10:30:21] INFO ✅ Migration completed: 2/2 users processed successfully
[2024-01-15 10:30:21] INFO 📊 Migration Summary:
[2024-01-15 10:30:21] INFO    Total users: 2
[2024-01-15 10:30:21] INFO    Processed: 2
[2024-01-15 10:30:21] INFO    Successful: 2
```

## Support

If you encounter issues, check:
1. Database connection string
2. MMN node is running and accessible
3. Faucet account has sufficient balance
4. Master key is in correct base64 format
5. Appropriate log level for debugging

For additional help:
- Review the logs for specific error messages
- Ensure all environment variables are properly set
- Verify the MMN blockchain node is synchronized
- Check database permissions and table existence
- Use `-log-level=debug` to see detailed process