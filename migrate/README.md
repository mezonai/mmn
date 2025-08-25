# MMN Blockchain Migration Tool

Tool migration này được sử dụng để migrate user data từ hệ thống cũ sang MMN blockchain, bao gồm tạo wallet và transfer token từ faucet.

## Tính năng
- ✅ **Tạo wallet tự động**: Tạo Ed25519 wallet cho tất cả users trong database
- ✅ **Transfer token**: Chuyển token từ faucet account đến user wallet
- ✅ **Mã hóa private key**: Lưu trữ private key được mã hóa AES-GCM trong database
- ✅ **Kiểm tra kết nối**: Tự động test kết nối database và blockchain
- ✅ **Nonce management**: Quản lý nonce tự động cho sequential transactions
- ✅ **Logging chi tiết**: Theo dõi quá trình migration với structured logging
- ✅ **Modular architecture**: Code được tổ chức thành các module riêng biệt
- ✅ **Upsert operations**: Tự động cập nhật wallet nếu đã tồn tại
- ✅ **Genesis faucet integration**: Sử dụng đúng faucet account từ genesis config
- ✅ **Transaction type compatibility**: Sử dụng FaucetTxType cho funding transactions

## Yêu cầu hệ thống

- Go 1.19+
- PostgreSQL database
- MMN blockchain node đang chạy
- gRPC connection đến MMN node

## Cấu hình

### Database
```sql
-- Bảng users (đã tồn tại)
CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    balance INTEGER NOT NULL
);

-- Bảng mmn_user_keys (sẽ được tạo tự động)
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
export DB_URL="postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable"

# MMN blockchain endpoint
export MMN_ENDPOINT="localhost:9002"

# Master key cho mã hóa (base64)
export MASTER_KEY="bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA="
```

## Cách sử dụng

### 1. Chạy migration cơ bản
```bash
cd migrate
go run .
```

### 2. Chạy với custom parameters
```bash
cd migrate
go run . \
  -endpoint="localhost:9002" \
  -db="postgres://user:pass@localhost:5432/db?sslmode=disable" \
  -master-key="your_base64_master_key"
```

### 3. Dry run (không thực hiện thay đổi)
```bash
cd migrate
go run . -dry-run=true
```

## Command Line Options

| Flag | Mô tả | Default |
|------|-------|----------|
| `-endpoint` | MMN blockchain gRPC endpoint | `localhost:9002` |
| `-db` | Database connection URL | `postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable` |
| `-master-key` | Master key cho mã hóa (base64) | `bWV6b25fdGVzdF9tYXN0ZXJfa2V5XzEyMzQ1Njc4OTA=` |
| `-dry-run` | Chỉ hiển thị thông tin, không thực hiện migration | `false` |

## Quy trình Migration

1. **Kiểm tra kết nối**
   - Test database connection
   - Test MMN blockchain gRPC connection
   - Tạo bảng `mmn_user_keys` nếu chưa tồn tại

2. **Lấy danh sách users**
   - Query users có `balance > 0`
   - Kiểm tra users đã có wallet chưa

3. **Tạo wallet**
   - Generate Ed25519 key pair
   - Mã hóa private key với master key
   - Lưu vào database với upsert operation
   - Tự động cập nhật `updated_at` timestamp

4. **Transfer tokens**
   - Lấy faucet account info
   - Build và sign transaction
   - Send transaction qua gRPC
   - Delay 2 giây giữa các transaction
   - Handle transaction broadcast errors gracefully

## Utility Scripts

### Clear wallets (để test)
```bash
# Xóa tất cả wallets trong database
psql "$DB_URL" -c "DELETE FROM mmn_user_keys;"

# Hoặc drop và recreate table
psql "$DB_URL" -c "DROP TABLE IF EXISTS mmn_user_keys;"
```

## Cấu trúc Files

```
migrate/
├── migrate.go          # Main entry point và orchestration
├── wallet.go           # Wallet creation và management functions
├── database.go         # Database operations và schema management
├── transfer.go         # Token transfer và transaction handling
├── types.go            # Struct definitions (Wallet, Tx, SignedTx)
├── config.go           # Configuration và constants
├── README.md           # Documentation này
└── go.mod              # Go module dependencies
```

## Module Architecture

### wallet.go
- `CreateWallet()`: Tạo Ed25519 key pair mới
- `GetFaucetAccount()`: Lấy faucet account từ genesis private key (đã sửa)
- `EncryptPrivateKey()`: Mã hóa private key với AES-GCM
- `DecryptPrivateKey()`: Giải mã private key từ database

### database.go
- `ConnectDB()`: Kết nối PostgreSQL database
- `CreateUserKeysTable()`: Tạo/recreate bảng mmn_user_keys
- `GetUsers()`: Lấy danh sách users cần migrate
- `CheckExistingWallet()`: Kiểm tra wallet đã tồn tại
- `SaveWallet()`: Lưu wallet với upsert operation

### transfer.go
- `TransferToUser()`: Thực hiện transfer token
- `BuildTransaction()`: Xây dựng transaction structure
- `SignTransaction()`: Ký transaction với private key
- `BroadcastTransaction()`: Gửi transaction lên blockchain

### types.go
- `Wallet`: Struct chứa thông tin wallet
- `Tx`: Transaction structure
- `SignedTx`: Signed transaction structure

### config.go
- Constants và configuration values
- Default parameters cho database và blockchain

## Security Notes

- ⚠️ **Master key**: Không commit master key vào git
- ⚠️ **Private keys**: Được mã hóa AES-GCM trước khi lưu database
- ⚠️ **Database credentials**: Sử dụng environment variables
- ✅ **Faucet private key**: Sử dụng đúng genesis faucet key từ config
- ⚠️ **Upsert operations**: Tự động cập nhật existing wallets
- ✅ **Transaction signing**: Đã implement đúng Ed25519 signature scheme

## Logs

Script sẽ output các thông tin sau:
- ✅ Kết nối database và blockchain thành công
- 📊 Số lượng users cần migrate (ví dụ: 3 users)
- 🔑 Wallet addresses được tạo cho từng user
- 💰 Token transfers thành công (1000 tokens mỗi user)
- 📈 Faucet balance và address tracking
- 🎯 Migration success rate (ví dụ: 3/3 users processed successfully)
- ⏱️ Thời gian hoàn thành migration

### Ví dụ output thành công:
```
Faucet Address: 0d1dfad29c20c13dccff213f52d2f98a395a0224b5159628d2bdb077cf4026a7
Faucet Balance: 1999999999
Processing user 1: alice -> 8373dee5a8b4c5e6f7890123456789abcdef0123
Processing user 2: bob -> 9484eff6b9c5d6f7a901234567890abcdef01234  
Processing user 3: charlie -> a595f007cad6e7f8ba12345678901bcdef012345
Migration completed successfully: 3/3 users processed
```

## Support

Nếu gặp vấn đề, kiểm tra:
1. Database connection string
2. MMN node đang chạy và accessible
3. Faucet account có đủ balance
4. Master key đúng format base64