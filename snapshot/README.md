# Snapshot Sync System - Peer-to-Peer Flow

## Tổng quan

Hệ thống snapshot sync mới cho phép các node tự động đồng bộ snapshot với nhau thông qua peer-to-peer discovery, không cần bootstrap node can thiệp.

## Luồng hoạt động mới

### 1. **Tự động discovery (mỗi 30 giây)**
```
Node → Kiểm tra cần snapshot → Tìm peers → Gửi snapshot info request
```

### 2. **Peer response**
```
Peer → Kiểm tra có snapshot → Gửi snapshot info response
```

### 3. **Download decision**
```
Node → Nhận response → Quyết định download → Bắt đầu UDP transfer
```

### 4. **UDP transfer**
```
Peer → Chunk snapshot → Node nhận chunks → Verify & assemble → Load vào DB
```

## Các thành phần chính

### **Snapshot Creator** (`snapshot.go`)
- Tạo snapshot mỗi 50 slots
- Tính toán bank hash
- Cleanup snapshot cũ

### **Snapshot Downloader** (`download.go`)
- Download snapshot từ peers
- UDP chunking với checksum
- Progress tracking và retry

### **UDP Streamer** (`udp_streamer.go`)
- Phục vụ snapshot requests
- Token authentication
- Configurable chunk size

### **Network Integration** (`p2p/network.go`)
- Tự động discovery peers
- Xử lý snapshot requests/responses
- Quản lý download tasks

## Message Types

### **Snapshot Info Request**
```json
{
  "type": "snapshot_info_request",
  "requester_id": "peer_id"
}
```

### **Snapshot Info Response**
```json
{
  "type": "snapshot_info_response",
  "has_snapshot": true,
  "peer_id": "peer_id",
  "file_size": 1024,
  "modified_time": 1234567890
}
```

## Cấu hình

### **Environment Variables**
- `SNAPSHOT_TOKEN`: Token xác thực
- `SNAPSHOT_CHUNK_SIZE`: Kích thước chunk (mặc định: 16KB)

### **Constants**
- `RangeForSnapshot`: Tần suất tạo snapshot (mặc định: 50 slots)
- Discovery interval: 30 giây

## Sử dụng

### **Khởi tạo trong node**
```go
// Trong p2p network setup
downloader := snapshot.NewSnapshotDownloader(dbProvider, "/data/snapshots")

// Trong node startup
snapshot.StartSnapshotUDPStreamer(dbProvider, "/data/snapshots", ":0")
```

### **Tự động sync**
- Node tự động kiểm tra cần snapshot mỗi 30 giây
- Tự động tìm peers có snapshot
- Tự động download và verify

## Ưu điểm của luồng mới

1. **Không phụ thuộc bootstrap**: Nodes tự sync với nhau
2. **Tự động discovery**: Không cần cấu hình thủ công
3. **Load balancing**: Có thể download từ nhiều peers
4. **Fault tolerance**: Tự động retry và failover
5. **Scalable**: Hoạt động tốt với nhiều nodes

## Troubleshooting

### **Common issues**
1. **UDP port conflicts**: Kiểm tra port không bị conflict
2. **Permission errors**: Kiểm tra quyền ghi `/data/snapshots`
3. **Network timeouts**: Tăng timeout nếu network chậm

### **Logs**
- `NETWORK:SNAPSHOT`: Discovery và sync logs
- `SNAPSHOT DOWNLOAD`: Download progress logs
- `SNAPSHOT:STREAMER`: UDP streaming logs

## Testing

```bash
# Chạy test
go test ./snapshot/... -v

# Build toàn bộ project
go build ./...
```
