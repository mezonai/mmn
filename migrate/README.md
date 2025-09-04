# MMN Blockchain Migration Tool

This tool provides two independent functions:

1. **Migration Wallet Creation**: Create migration wallet, fund it with total users balance from faucet, and save to file
2. **User Migration**: Migrate user data from database to MMN blockchain using migration wallet for transfers

## Features

- ✅ **Independent wallet creation and migration processes**: Can run migration wallet creation and user migration separately
- ✅ **Smart migration wallet creation**: Creates wallet using CreateKey method (without saving to database) and stores as file
- ✅ **Automatic funding**: Migration wallet is automatically funded with total users balance from faucet
- ✅ **File-based transfers**: Migration uses migration wallet from file instead of faucet
- ✅ **Automatic wallet creation**: Create Ed25519 wallets for all users in database using CreateKey with database save
- ✅ **Token transfers**: Transfer tokens from migration wallet to user wallets
- ✅ **AES-GCM private key encryption**: Store AES-GCM encrypted private keys in database for user wallets
- ✅ **Connection checking**: Automatically check database and blockchain connections
- ✅ **Automatic nonce management**: Manage nonce automatically to avoid transaction conflicts
- ✅ **Detailed structured logging**: Track migration process with colored structured logging and timestamps
- ✅ **Modular architecture**: Code organized into separate modules (wallet, database, transfer, logger)

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
    username VARCHAR(255) NOT NULL,
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

### Configuration Constants

The tool uses configurable constants that can be easily modified:

To change the migration wallet identifier, modify the constant in the relevant files.

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

### 1. Create migration wallet (Requires database connection)

```bash
cd migrate

# Dry-run mode
go run . -create-wallet -dry-run

# Create actual wallet (creates wallet, funds it, saves to file)
go run . -create-wallet
```

### 2. Run user migration (Requires database and migration wallet file)

```bash
cd migrate

# Dry-run mode
go run . -migrate -dry-run

# Run actual migration
go run . -migrate
```

### 3. Run both wallet creation and migration

```bash
cd migrate
go run . -dry-run  # Both wallet creation and migration in dry-run
go run .           # Both wallet creation and migration
```

### 4. Log level control

```bash
cd migrate
go run . -log-level=debug   # Detailed debug information
go run . -log-level=info    # Standard information (default)
go run . -log-level=warn    # Warnings and errors only
```

## Command Line Options

| Option           | Description                                 | Default Value |
| ---------------- | ------------------------------------------- | ------------- |
| `-create-wallet` | Create migration wallet only                | `false`       |
| `-migrate`       | Run user migration only                     | `false`       |
| `-dry-run`       | Run without making actual changes           | `false`       |
| `-log-level`     | Log level (debug, info, warn, error, fatal) | `info`        |

**Note**: If neither `-create-wallet` nor `-migrate` is specified, both will run by default.

**Environment Variables**:

- `DATABASE_URL`: Database connection string
- `MMN_ENDPOINT`: MMN blockchain gRPC endpoint
- `MASTER_KEY`: Master key for encryption (base64)

## Migration Process

1. **Migration Wallet Creation** (Independent process)

   - Create new Ed25519 wallet using CreateKey method (without saving to database)
   - Get wallet address from public key (base58 encoded)
   - Save private key to file: `wallets/{address}` (content: private key in hex)
   - Calculate total wallet balance of all users in database
   - Transfer total users balance from faucet to migration wallet
   - Verify migration wallet has sufficient balance on blockchain

2. **User Migration** (Independent process)

   - Check database connection
   - Check MMN blockchain gRPC connection
   - Create `mmn_user_keys` table if not exists
   - Load migration wallet from file (`wallets/` directory)
   - Query all users from database (excluding migration wallet user)
   - For each user:
     - Check if wallet exists in database, create using CreateKey (with database save) if not
     - Get current blockchain balance
     - Compare with database balance
     - Transfer shortfall from migration wallet
     - Skip users that already have sufficient balance

## Technical Details

### CreateKey Method

The `CreateKey` method has been enhanced with an `isSave` parameter:

- `CreateKey(userID, true)`: Creates wallet and saves to database (for user wallets)
- `CreateKey(userID, false)`: Creates wallet without saving to database (for migration wallet)

### Migration Wallet

- **UserID**: 0 (special ID for migration wallet)
- **Storage**: File-based only (`wallets/{address}` containing private key in hex)
- **Funding**: Automatically funded with total users balance from faucet
- **Purpose**: Source wallet for transferring tokens to user wallets during migration

### User Wallets

- **UserID**: Actual user IDs from database
- **Storage**: Database with AES-GCM encrypted private keys
- **Creation**: Uses CreateKey with database save enabled

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
└── README.md           # This documentation
```

## Module Architecture

### migrate.go

- `main()`: Main entry point, orchestrate entire migration process
- `parseLogLevel()`: Convert string log level to LogLevel enum
- Handle command line flags and initialize logger

### wallet.go

- `CreateMigrationWallet()`: Create migration wallet with automatic funding from faucet
- `CreateKey(userID uint64, isSave bool)`: Enhanced key creation with database save control
- `GetMigrationWalletFromFile()`: Reads migration wallet from file
- `GetFaucetAccount()`: Get faucet account from genesis private key
- `NewPgEncryptedStore()`: Create wallet manager with AES-GCM encryption
- `LoadKey()`: Load private key from database and decrypt
- `encrypt()/decrypt()`: Encrypt/decrypt private key with AES-GCM

### database.go

- `ConnectDatabase()`: Connect to PostgreSQL with retry mechanism
- `CreateUserKeysTable()`: Create/recreate mmn_user_keys table
- `GetUsers()`: Get list of users to migrate (excludes migration wallet)
- `GetTotalUsersWallet()`: Calculate total wallet balance for all users
- `CheckExistingWallet()`: Check if wallet already exists
- `CountExistingWallets()`: Count existing wallets

### transfer.go

- `TransferTokens()`: Legacy token transfer function (still available)
- `GetAccountByAddress()`: Get account information from blockchain
- `defaultClient()`: Create MMN client to communicate with blockchain
- Uses Ed25519 signature and proper transaction building

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

- ⚠️ **Master key**: Do not commit master key to git, use environment variables
- ✅ **Private keys**: User wallet private keys are AES-GCM encrypted before saving to database
- ✅ **Migration wallet**: Private key saved as plain text to file (secure file permissions required)
- ⚠️ **Database credentials**: Use environment variables, never hardcode
- ✅ **Faucet integration**: Uses proper genesis faucet configuration
- ✅ **Transaction signing**: Properly implemented Ed25519 signature scheme
- ✅ **Structured logging**: Logs do not contain sensitive information like private keys
- ⚠️ **File permissions**: Ensure migration wallet files have appropriate permissions (600)

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
[2024-01-15 10:30:15] INFO 🔗 Database connection successful (endpoint: localhost:5432)
[2024-01-15 10:30:16] INFO ✅ mmn_user_keys table ready
[2024-01-15 10:30:16] INFO 📊 Creating migration wallet...
[2024-01-15 10:30:16] INFO 💰 Migration wallet created - Address: 7364bce4a9b3c4d5e6f7890123456789abcdef012
[2024-01-15 10:30:16] INFO 💸 Funding migration wallet with total users balance: 2500 tokens
[2024-01-15 10:30:17] INFO ✅ Migration wallet funded successfully
[2024-01-15 10:30:17] INFO 🚀 Starting migration process for 2 users
[2024-01-15 10:30:18] INFO 💰 Wallet created for user 1 - Address: 8373dee5a8b4c5e6f7890123456789abcdef0123
[2024-01-15 10:30:18] INFO 💸 Token transfer: 7364bce... → 8373dee... (amount: 1000)
[2024-01-15 10:30:19] INFO 💰 Wallet created for user 2 - Address: 9484eff6b9c5d6f7a901234567890abcdef01234
[2024-01-15 10:30:19] INFO 💸 Token transfer: 7364bce... → 9484eff... (amount: 1500)
[2024-01-15 10:30:21] INFO ✅ Migration completed: 2/2 users processed successfully
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
