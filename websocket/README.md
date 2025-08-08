# MMN WebSocket Events Service

Dịch vụ WebSocket này cung cấp khả năng lắng nghe real-time các events của transactions và blocks trong mạng MMN.

## 🚀 Tính năng

- **Real-time Transaction Events**: Theo dõi transactions khi được submit, confirm hoặc fail
- **Block Events**: Theo dõi khi có block mới được tạo
- **Mempool Statistics**: Thống kê real-time về trạng thái mempool
- **WebSocket Connection Management**: Tự động kết nối lại và quản lý client connections
- **Event Broadcasting**: Phát events đến tất cả clients đã kết nối

## 📡 Event Types

### 1. Transaction Events

#### `tx_submitted` - Transaction được submit vào mempool
```json
{
  "type": "tx_submitted",
  "timestamp": 1704880800,
  "data": {
    "tx_hash": "abcd1234...",
    "tx_data": {
      "type": 0,
      "sender": "sender_pubkey",
      "recipient": "recipient_pubkey", 
      "amount": 1000,
      "timestamp": 1704880800,
      "text_data": "payment",
      "nonce": 1
    }
  }
}
```

#### `tx_confirmed` - Transaction được confirm trong block
```json
{
  "type": "tx_confirmed", 
  "timestamp": 1704880800,
  "data": {
    "tx_hash": "abcd1234...",
    "tx_data": { /* transaction data */ },
    "slot": 12345
  }
}
```

#### `tx_failed` - Transaction thất bại
```json
{
  "type": "tx_failed",
  "timestamp": 1704880800, 
  "data": {
    "tx_hash": "abcd1234...",
    "tx_data": { /* transaction data */ },
    "error": "insufficient balance"
  }
}
```

### 2. Block Events

#### `block_created` - Block mới được tạo
```json
{
  "type": "block_created",
  "timestamp": 1704880800,
  "data": {
    "slot": 12345,
    "tx_count": 5,
    "transactions": ["hash1", "hash2", "hash3", "hash4", "hash5"]
  }
}
```

### 3. System Events

#### `mempool_stats` - Thống kê mempool
```json
{
  "type": "mempool_stats",
  "timestamp": 1704880800,
  "data": {
    "tx_count": 10,
    "tx_hashes": ["hash1", "hash2", "..."],
    "max_size": 1000,
    "is_full": false
  }
}
```

## 🔧 Cách sử dụng

### 1. Chạy MMN Node với WebSocket
```bash
go run cmd/main.go -node=node1
```

WebSocket server sẽ chạy trên port `8090` (có thể thay đổi trong code).

### 2. Kết nối WebSocket

#### JavaScript Client
```javascript
const ws = new WebSocket('ws://localhost:8090/ws');

ws.onopen = function() {
    console.log('Connected to MMN WebSocket');
};

ws.onmessage = function(event) {
    const eventData = JSON.parse(event.data);
    console.log('Received event:', eventData);
    
    switch(eventData.type) {
        case 'tx_submitted':
            console.log('New transaction submitted:', eventData.data.tx_hash);
            break;
        case 'tx_confirmed': 
            console.log('Transaction confirmed in slot:', eventData.data.slot);
            break;
        case 'block_created':
            console.log('New block created:', eventData.data.slot);
            break;
    }
};

ws.onclose = function() {
    console.log('Disconnected from MMN WebSocket');
};
```

#### Python Client
```python
import asyncio
import websockets
import json

async def listen_events():
    uri = "ws://localhost:8090/ws"
    async with websockets.connect(uri) as websocket:
        print("Connected to MMN WebSocket")
        
        async for message in websocket:
            event = json.loads(message)
            print(f"Received event: {event['type']}")
            
            if event['type'] == 'tx_submitted':
                print(f"New transaction: {event['data']['tx_hash']}")
            elif event['type'] == 'block_created':
                print(f"New block: slot {event['data']['slot']}")

asyncio.run(listen_events())
```

### 3. HTML Monitor Client

Mở file `websocket/client.html` trong browser để có giao diện web theo dõi real-time:

```bash
# Serve static file (optional)
python3 -m http.server 8080
# Hoặc mở trực tiếp file:///path/to/websocket/client.html
```

## 🛠️ API Endpoints

### WebSocket Endpoint
- **URL**: `ws://localhost:8090/ws`
- **Protocol**: WebSocket
- **Format**: JSON

### Health Check
- **URL**: `http://localhost:8090/health`
- **Method**: GET
- **Response**: 
```json
{
  "status": "ok",
  "clients": 5,
  "timestamp": 1704880800
}
```

## 🏗️ Kiến trúc

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   MMN Node      │    │  Event Bus      │    │ WebSocket       │
│                 │    │                 │    │ Server          │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │  Mempool    │─┼────┼▶│ Tx Events   │─┼────┼▶│  Broadcast  │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │ Validator   │─┼────┼▶│Block Events │─┼────┼▶│   Clients   │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## 🔒 Bảo mật

- **CORS**: Hiện tại allow all origins (development only)
- **Rate Limiting**: Chưa implement (cần thêm cho production)
- **Authentication**: Chưa có (cần thêm cho production)

Cho production environment, nên thêm:
- Authentication/Authorization
- Rate limiting
- CORS restrictions
- Message filtering theo user permissions

## 📊 Performance

- **Connection Limit**: Không giới hạn (cần monitor trong production)
- **Message Buffer**: 256 events per client
- **Broadcast Interval**: Mempool stats mỗi 5 giây
- **Ping/Pong**: 54 giây interval để keep-alive

## 🐛 Troubleshooting

### WebSocket không kết nối được
1. Kiểm tra MMN node đã chạy chưa
2. Kiểm tra port 8090 có bị block không
3. Kiểm tra firewall settings

### Không nhận được events
1. Kiểm tra WebSocket connection status
2. Thử submit transaction để test
3. Kiểm tra logs của MMN node

### Performance issues
1. Giảm số clients kết nối
2. Increase buffer sizes
3. Implement event filtering

## 📝 Example Use Cases

### 1. Transaction Monitor Dashboard
```javascript
// Monitor all transactions
ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    if (data.type === 'tx_submitted') {
        addToTransactionList(data.data);
    }
};
```

### 2. Block Explorer
```javascript
// Track new blocks
ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    if (data.type === 'block_created') {
        updateBlockExplorer(data.data);
    }
};
```

### 3. Mempool Analytics
```javascript
// Monitor mempool health
ws.onmessage = function(event) {
    const data = JSON.parse(event.data);
    if (data.type === 'mempool_stats') {
        updateMempoolChart(data.data);
    }
};
```

## 🚧 Future Enhancements

- [ ] Event filtering và subscription theo topic
- [ ] Authentication và authorization
- [ ] Rate limiting cho clients
- [ ] Persistent event storage
- [ ] Event replay functionality
- [ ] Metrics và monitoring endpoints
- [ ] Load balancing multiple WebSocket servers
- [ ] Event compression cho large payloads
