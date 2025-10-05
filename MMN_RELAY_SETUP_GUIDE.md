# MMN Relay Setup trên Google Cloud Platform

## Tổng quan

Document này hướng dẫn setup 3 node MMN trên Google Cloud Platform với relay service để mô phỏng network thực tế trên internet.

## Kiến trúc hệ thống

```
Internet
    ↓
lậo
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Yêu cầu hệ thống

- Google Cloud Platform account
- gcloud CLI đã cài đặt và authenticated
- Docker đã cài đặt trên máy local
- Go 1.24+ (để build binary)

## Bước 1: Chuẩn bị môi trường

### 1.1 Kiểm tra cấu hình gcloud
```bash
gcloud config list
```

**Kết quả mong đợi:**
```
[core]
account = your-email@gmail.com
project = your-project-id
```

### 1.2 Build Docker image với LevelDB support
```bash
# Build image cho Linux AMD64
docker buildx create --use
docker buildx build --platform linux/amd64 -f Dockerfile.leveldb -t gcr.io/YOUR_PROJECT_ID/mmn-node:leveldb --push .
```

**Lưu ý:** Thay `YOUR_PROJECT_ID` bằng project ID thực tế của bạn.

## Bước 2: Tạo 3 VM instances

### 2.1 Tạo Bootnode VM
```bash
gcloud compute instances create mmn-bootnode \
    --zone=asia-southeast1-a \
    --machine-type=e2-micro \
    --image-family=ubuntu-2204-lts \
    --image-project=ubuntu-os-cloud \
    --tags=mmn-bootnode \
    --metadata=startup-script='#!/bin/bash
apt-get update
apt-get install -y docker.io git
systemctl start docker
systemctl enable docker
usermod -aG docker $USER'
```

### 2.2 Tạo Node1 VM
```bash
gcloud compute instances create mmn-node1 \
    --zone=asia-southeast1-b \
    --machine-type=e2-micro \
    --image-family=ubuntu-2204-lts \
    --image-project=ubuntu-os-cloud \
    --tags=mmn-node \
    --metadata=startup-script='#!/bin/bash
apt-get update
apt-get install -y docker.io git
systemctl start docker
systemctl enable docker
usermod -aG docker $USER'
```

### 2.3 Tạo Listener VM
```bash
gcloud compute instances create mmn-listener \
    --zone=asia-southeast1-c \
    --machine-type=e2-micro \
    --image-family=ubuntu-2204-lts \
    --image-project=ubuntu-os-cloud \
    --tags=mmn-node \
    --metadata=startup-script='#!/bin/bash
apt-get update
apt-get install -y docker.io git
systemctl start docker
systemctl enable docker
usermod -aG docker $USER'
```

### 2.4 Lấy External IP addresses
```bash
gcloud compute instances list --filter="name~mmn-"
```

**Kết quả mong đợi:**
```
NAME          ZONE               INTERNAL_IP  EXTERNAL_IP    STATUS
mmn-bootnode  asia-southeast1-a  10.148.0.3   34.87.33.46    RUNNING
mmn-node1     asia-southeast1-b  10.148.0.4   34.87.105.137  RUNNING
mmn-listener  asia-southeast1-c  10.148.0.5   35.240.216.94  RUNNING
```

**Lưu ý quan trọng:** Ghi lại các EXTERNAL_IP này để sử dụng trong cấu hình bootstrap.

## Bước 3: Cấu hình Firewall Rules

### 3.1 Tạo firewall rule cho Bootnode
```bash
gcloud compute firewall-rules create allow-mmn-bootnode \
    --allow tcp:9000,udp:9000 \
    --source-ranges 0.0.0.0/0 \
    --target-tags mmn-bootnode
```

### 3.2 Tạo firewall rule cho Nodes
```bash
gcloud compute firewall-rules create allow-mmn-nodes \
    --allow tcp:8001,tcp:8081,tcp:9001,tcp:8004,tcp:8084,tcp:9004,tcp:9005 \
    --source-ranges 0.0.0.0/0 \
    --target-tags mmn-node
```

## Bước 4: Deploy Bootnode

### 4.1 Build binary cho Linux
```bash
GOOS=linux GOARCH=amd64 go build -o mmn-linux .
```

### 4.2 Copy files lên Bootnode VM
```bash
gcloud compute scp --zone=asia-southeast1-a ./mmn-linux mmn-bootnode:~/mmn
gcloud compute scp --zone=asia-southeast1-a --recurse ./config mmn-bootnode:~/config
```

### 4.3 Chạy Bootnode
```bash
gcloud compute ssh mmn-bootnode --zone=asia-southeast1-a --command="
chmod +x ~/mmn
cd ~
export LOGFILE_MAX_SIZE_MB=500
export LOGFILE_MAX_AGE_DAYS=7
nohup ./mmn bootnode --bootstrap-p2p-port 9000 --privkey-path config/bootnode_privkey.txt > bootnode.log 2>&1 &
echo 'Bootnode started!'
sleep 3
ps aux | grep bootnode
"
```

### 4.4 Kiểm tra Bootnode
```bash
gcloud compute ssh mmn-bootnode --zone=asia-southeast1-a --command="
netstat -tulpn | grep 9000
tail -5 bootnode.log
"
```

**Kết quả mong đợi:**
```
tcp        0      0 0.0.0.0:9000            0.0.0.0:*               LISTEN      3686/./mmn
udp        0      0 0.0.0.0:9000            0.0.0.0:*                           3686/./mmn
```

## Bước 5: Deploy Node1

### 5.1 Configure Docker authentication trên VM
```bash
gcloud compute ssh mmn-node1 --zone=asia-southeast1-b --command="
sudo gcloud auth configure-docker
"
```

### 5.2 Deploy Node1 container
```bash
gcloud compute ssh mmn-node1 --zone=asia-southeast1-b --command="
sudo docker pull gcr.io/YOUR_PROJECT_ID/mmn-node:leveldb
sudo docker run -d --name node1 --network host \
    -e LOGFILE_MAX_SIZE_MB=500 \
    -e LOGFILE_MAX_AGE_DAYS=7 \
    gcr.io/YOUR_PROJECT_ID/mmn-node:leveldb \
    sh -c '
    ./mmn init --data-dir \"./node-data/node1\" --genesis \"config/genesis.yml\" --database \"leveldb\" --privkey-path \"config/key1.txt\" &&
    ./mmn node --database leveldb --data-dir \"./node-data/node1\" --listen-addr \":8001\" --jsonrpc-addr \":8081\" --grpc-addr \":9001\" --bootstrap-addresses \"/ip4/BOOTNODE_EXTERNAL_IP/udp/9000/quic-v1/p2p/BOOTNODE_PEER_ID\"
    '
echo 'Node1 deployed!'
sudo docker ps
"
```

**Lưu ý:** Thay `BOOTNODE_EXTERNAL_IP` bằng IP thực tế của bootnode (34.87.33.46) và `BOOTNODE_PEER_ID` bằng peer ID thực tế.

## Bước 6: Deploy Listener

### 6.1 Configure Docker authentication
```bash
gcloud compute ssh mmn-listener --zone=asia-southeast1-c --command="
sudo gcloud auth configure-docker
"
```

### 6.2 Deploy Listener container
```bash
gcloud compute ssh mmn-listener --zone=asia-southeast1-c --command="
sudo docker pull gcr.io/YOUR_PROJECT_ID/mmn-node:leveldb
sudo docker run -d --name listener --network host \
    -e LOGFILE_MAX_SIZE_MB=500 \
    -e LOGFILE_MAX_AGE_DAYS=7 \
    gcr.io/YOUR_PROJECT_ID/mmn-node:leveldb \
    sh -c '
    ./mmn init --data-dir \"./node-data/listener\" --genesis \"config/genesis.yml\" --database \"leveldb\" --privkey-path \"config/listener.txt\" &&
    ./mmn node --database leveldb --data-dir \"./node-data/listener\" --listen-addr \":8004\" --jsonrpc-addr \":8084\" --grpc-addr \":9004\" --p2p-port \"9005\" --mode listen --bootstrap-addresses \"/ip4/BOOTNODE_EXTERNAL_IP/udp/9000/quic-v1/p2p/BOOTNODE_PEER_ID\"
    '
echo 'Listener deployed!'
sudo docker ps
"
```

## Bước 7: Kiểm tra kết nối

### 7.1 Kiểm tra logs của Node1
```bash
gcloud compute ssh mmn-node1 --zone=asia-southeast1-b --command="
sudo docker exec node1 tail -10 /app/logs/mmn.log
"
```

**Kết quả mong đợi:**
```
2025/10/05 06:15:22.082284 [INFO][VOTE]: Block finalized via P2P! slot= 4942
2025/10/05 06:15:22.131591 [INFO][LEADER]: Pulling batch for slot 4943
2025/10/05 06:15:22.131679 [INFO][MEMPOOL]: No more ready transactions found, processed 0
```

### 7.2 Kiểm tra peer connections
```bash
gcloud compute ssh mmn-node1 --zone=asia-southeast1-b --command="
sudo docker exec node1 grep -E '(peer|connect|bootstrap|P2P)' /app/logs/mmn.log | tail -5
"
```

**Kết quả mong đợi:**
```
2025/10/05 06:16:12.482416 [INFO][BLOCK]:   Block mesh peer 1: 12D3KooWPWimRLiXdS8x8F8vrY4ZTTDvwvPhspAHZ5v9FjWBYW57
2025/10/05 06:16:12.483073 [INFO][VOTE]: Block finalized via P2P! slot= 5068
```

### 7.3 Kiểm tra logs của Listener
```bash
gcloud compute ssh mmn-listener --zone=asia-southeast1-c --command="
sudo docker exec listener grep -E '(peer|connect|bootstrap|P2P)' /app/logs/mmn.log | tail -5
"
```

**Kết quả mong đợi:**
```
2025/10/05 06:18:07.683570 [INFO][NETWORK:BLOCK]: Received block from peer: 12D3KooWGyLEeWJT9tVeNQtML4q8yPKCWkk5QtTtSYDLM6kHTLP4 slot: 5356
2025/10/05 06:18:08.083448 [INFO][NETWORK:BLOCK]: Received block from peer: 12D3KooWGyLEeWJT9tVeNQtML4q8yPKCWkk5QtTtSYDLM6kHTLP4 slot: 5357
```

## Cách lấy địa chỉ IP chính xác

### Lấy External IP của tất cả instances
```bash
gcloud compute instances list --filter="name~mmn-" --format="table(name,zone,EXTERNAL_IP)"
```

### Lấy External IP của bootnode cụ thể
```bash
gcloud compute instances describe mmn-bootnode --zone=asia-southeast1-a --format="get(networkInterfaces[0].accessConfigs[0].natIP)"
```

### Lấy External IP của node1
```bash
gcloud compute instances describe mmn-node1 --zone=asia-southeast1-b --format="get(networkInterfaces[0].accessConfigs[0].natIP)"
```

### Lấy External IP của listener
```bash
gcloud compute instances describe mmn-listener --zone=asia-southeast1-c --format="get(networkInterfaces[0].accessConfigs[0].natIP)"
```

## Kết quả thành công

Khi setup thành công, bạn sẽ thấy:

✅ **Bootnode**: Đang chạy relay service trên port 9000  
✅ **Node1**: Đang tạo blocks và gửi qua P2P network  
✅ **Listener**: Đang nhận blocks từ Node1 qua P2P network  
✅ **P2P Communication**: Blocks được sync thành công  
✅ **Relay Service**: Hoạt động qua NAT/firewall  

## Troubleshooting

### Nếu không kết nối được:
1. Kiểm tra firewall rules
2. Kiểm tra external IP addresses
3. Kiểm tra peer ID trong bootstrap addresses
4. Kiểm tra logs chi tiết trong `/app/logs/mmn.log`

### Kiểm tra logs chi tiết:
```bash
# Bootnode logs
gcloud compute ssh mmn-bootnode --zone=asia-southeast1-a --command="tail -20 bootnode.log"

# Node1 logs
gcloud compute ssh mmn-node1 --zone=asia-southeast1-b --command="sudo docker exec node1 tail -20 /app/logs/mmn.log"

# Listener logs
gcloud compute ssh mmn-listener --zone=asia-southeast1-c --command="sudo docker exec listener tail -20 /app/logs/mmn.log"
```

## Cleanup

Để dọn dẹp resources:
```bash
gcloud compute instances delete mmn-bootnode mmn-node1 mmn-listener --zone=asia-southeast1-a --zone=asia-southeast1-b --zone=asia-southeast1-c --quiet
gcloud compute firewall-rules delete allow-mmn-bootnode allow-mmn-nodes --quiet
```

## Kết luận

Setup này mô phỏng thành công 3 node MMN trên internet thực tế với:
- **NAT traversal** qua relay service
- **Firewall handling** với proper rules
- **Network latency** thực tế giữa các zones
- **P2P communication** hoạt động ổn định

Đây là foundation tốt để test và develop các tính năng P2P network của MMN.
