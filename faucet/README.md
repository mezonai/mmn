# Multisig Faucet System

Hệ thống faucet multisig cung cấp bảo mật cao cho việc phân phối token thông qua yêu cầu nhiều chữ ký.

## Tính năng chính

- **Multisig Address**: Tạo địa chỉ từ tập hợp public keys của các thành viên
- **Quy tắc ký m-of-n**: Cần ít nhất m chữ ký trong n thành viên để phê duyệt
- **Xác thực bảo mật**: Kiểm tra địa chỉ multisig, số lượng chữ ký, và tính hợp lệ của từng chữ ký
- **API RESTful**: Giao diện HTTP để tương tác với hệ thống
- **CLI Commands**: Command line interface để quản lý

## Cấu trúc

```
faucet/
├── multisig.go      # Core multisig logic
├── service.go       # Faucet service management
├── api.go          # HTTP API endpoints
├── example.go      # Usage examples
└── README.md       # Documentation
```

## Cách sử dụng

### 1. Tạo Multisig Configuration

```bash
# Tạo cấu hình 2-of-3 multisig
./mmn faucet-multisig register --threshold 2 --signers "pubkey1,pubkey2,pubkey3"
```

### 2. Tạo Faucet Request

```bash
# Tạo yêu cầu faucet
./mmn faucet-multisig create \
  --multisig-address "multisig_address_here" \
  --recipient "recipient_address" \
  --amount "100000" \
  --text-data "Test request"
```

### 3. Thêm Chữ ký

```bash
# Thêm chữ ký từ signer đầu tiên
./mmn faucet-multisig sign \
  --tx-hash "transaction_hash" \
  --signer-pubkey "pubkey1" \
  --private-key "private_key1"

# Thêm chữ ký từ signer thứ hai
./mmn faucet-multisig sign \
  --tx-hash "transaction_hash" \
  --signer-pubkey "pubkey2" \
  --private-key "private_key2"
```

### 4. Thực thi Giao dịch

```bash
# Thực thi giao dịch khi đủ chữ ký
./mmn faucet-multisig execute --tx-hash "transaction_hash"
```

### 5. Kiểm tra Trạng thái

```bash
# Xem trạng thái giao dịch
./mmn faucet-multisig status --tx-hash "transaction_hash"

# Liệt kê tất cả giao dịch đang chờ
./mmn faucet-multisig list-pending

# Xem thống kê dịch vụ
./mmn faucet-multisig stats
```

## API Endpoints

### POST /multisig/register
Đăng ký cấu hình multisig mới

```json
{
  "threshold": 2,
  "signers": ["pubkey1", "pubkey2", "pubkey3"]
}
```

### POST /multisig/faucet/create
Tạo yêu cầu faucet mới

```json
{
  "multisig_address": "multisig_address",
  "recipient": "recipient_address",
  "amount": "100000",
  "text_data": "Optional text"
}
```

### POST /multisig/faucet/sign
Thêm chữ ký vào giao dịch

```json
{
  "tx_hash": "transaction_hash",
  "signer_pub_key": "signer_public_key",
  "private_key": "signer_private_key"
}
```

### POST /multisig/faucet/execute
Thực thi giao dịch đã được ký đầy đủ

```json
{
  "tx_hash": "transaction_hash"
}
```

### GET /multisig/faucet/status?tx_hash=...
Lấy trạng thái giao dịch

### GET /multisig/faucet/stats
Lấy thống kê dịch vụ

### GET /multisig/faucet/pending
Liệt kê tất cả giao dịch đang chờ

## Ví dụ Code

```go
package main

import (
    "github.com/mezonai/mmn/faucet"
    "github.com/holiman/uint256"
)

func main() {
    // Tạo cấu hình multisig
    config, err := faucet.CreateMultisigConfig(2, []string{"pubkey1", "pubkey2", "pubkey3"})
    if err != nil {
        panic(err)
    }

    // Tạo dịch vụ faucet
    maxAmount, _ := uint256.FromDecimal("1000000")
    service := faucet.NewMultisigFaucetService(maxAmount, 1*time.Hour)
    
    // Đăng ký cấu hình
    service.RegisterMultisigConfig(config)

    // Tạo yêu cầu faucet
    amount, _ := uint256.FromDecimal("100000")
    tx, err := service.CreateFaucetRequest(config.Address, "recipient", amount, "Test")
    if err != nil {
        panic(err)
    }

    // Thêm chữ ký (cần 2 chữ ký)
    service.AddSignature(tx.Hash(), "pubkey1", privateKey1)
    service.AddSignature(tx.Hash(), "pubkey2", privateKey2)

    // Thực thi giao dịch
    executedTx, err := service.VerifyAndExecute(tx.Hash())
    if err != nil {
        panic(err)
    }
}
```

## Bảo mật

- **Xác thực chữ ký**: Mỗi chữ ký được xác thực bằng Ed25519
- **Kiểm tra quyền**: Chỉ các signer được ủy quyền mới có thể ký
- **Giới hạn số lượng**: Có thể đặt giới hạn số lượng token tối đa
- **Cooldown**: Có thể đặt thời gian chờ giữa các yêu cầu
- **Xác thực địa chỉ**: Kiểm tra định dạng địa chỉ hợp lệ

## Lưu ý

- Private keys phải được bảo mật cẩn thận
- Nên sử dụng hardware wallet cho các signer quan trọng
- Cấu hình multisig phải được lưu trữ an toàn
- Thường xuyên kiểm tra và cập nhật danh sách signer nếu cần
