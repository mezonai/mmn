# MMN Blockchain - Quick Start Guide

## 🚀 Chạy Nhanh (5 phút)

### 1. Build và Chạy Node

```bash
# Build project
go build -o mmn .

# Tạo data directory
mkdir -p node-data

# Khởi tạo node
./mmn init --data-dir node-data

# Chạy node
./mmn node --data-dir node-data --p2p-port 9000 --grpc-port 50051
```

### 2. Chạy Performance Test

```bash
# Mở terminal mới
cd client_test/mezon-server-sim/performance-test

# Set environment variables
export LOGFILE_MAX_SIZE_MB=100
export LOGFILE_MAX_AGE_DAYS=7

# Chạy test
go test -v -run TestPerformance_SendToken
```

### 3. Kết quả Mong đợi

```
=== TPS METRICS ===
Ingress TPS:  3364.09
Executed TPS: 191.23
Finalized TPS: 191.21

=== PERFORMANCE ASSESSMENT ===
✅ Ingress TPS: EXCELLENT (>1000 TPS)
🟡 Executed TPS: GOOD (100-500 TPS)
✅ Overall Efficiency: EXCELLENT (>90%)
```

## 🔧 Configuration Tối ưu

### genesis.yml (Đã được tối ưu)
```yaml
poh:
  hashes_per_tick: 20      # Tối ưu từ 10
  ticks_per_slot: 2        # Tối ưu từ 4
  tick_interval_ms: 50     # Tối ưu từ 100

validator:
  batch_size: 2000         # Tối ưu từ 750
  leader_timeout: 25       # Tối ưu từ 50
  leader_timeout_loop_interval: 2
```

## 📊 Performance Comparison

| Metric | Before | After | Improvement |
|--------|--------|-------|-------------|
| Ingress TPS | 2,011 | **3,364** | +67% |
| Executed TPS | 97 | **191** | +97% |
| Finalized TPS | 97 | **191** | +97% |

## 🐛 Troubleshooting

### Port đã được sử dụng
```bash
# Kill process sử dụng port
lsof -i :50051 | grep LISTEN | awk '{print $2}' | xargs kill -9
lsof -i :9000 | grep LISTEN | awk '{print $2}' | xargs kill -9
```

### Permission denied
```bash
chmod +x mmn
```

### Database lock
```bash
rm -rf node-data/store/*.lock
```

## 📈 Monitoring

```bash
# Xem logs real-time
tail -f logs/mmn.log

# Xem performance metrics
tail -f logs/mmn.log | grep "VALIDATOR_METRICS"
```

## 🎯 Next Steps

1. **Chạy Multiple Nodes**: Tham khảo `OPTIMIZATION_GUIDE.md`
2. **Custom Configuration**: Chỉnh sửa `config/genesis.yml`
3. **Performance Tuning**: Điều chỉnh các tham số trong config
4. **Monitoring**: Sử dụng các tools monitoring được đề xuất

---

**Lưu ý**: Để biết thêm chi tiết, xem `OPTIMIZATION_GUIDE.md`
