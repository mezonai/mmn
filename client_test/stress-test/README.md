### Stress test runner

Use the helper script `load-txs.sh` to start the load generator in the background with logging and a saved PID.

```bash
./load-txs.sh [accounts] [rate] [fund] [amount] [duration_seconds] [minutes] [total_txs] [extra flags...]
```

- **logs**: output goes to `client_test/stress-test/logs/load_test_<timestamp>.log`
- **pid**: background PID is saved to `client_test/stress-test/load_test.pid`
- **log reports**: generated log reports are saved in `client_test/stress-test/reports/log_reports`
- **html reports**: generated HTML reports are saved in `client_test/stress-test/reports/html_reports`

### Quick examples

```bash
# Default-ish run (uses script defaults)
./load-txs.sh

# 1) 1,000 accounts at 500 TPS for 10 minutes
./load-txs.sh 1000 500 10000000000 100 0 10 0

# 2) Open-ended run at 100 TPS
./load-txs.sh 300 100 10000000000 100 0 0 0

# 3) Stop after exactly 50,000 transactions
./load-txs.sh 300 200 10000000000 100 0 0 50000

# 4) Run with private-key transfers instead of zk proof
./load-txs.sh 300 200 10000000000 100 0 0 0 -use-key

# 5) Inject 5 invalid nonce tx/s and 3 duplicate tx/s
./load-txs.sh 300 200 10000000000 100 0 0 0 -err-nonce 5 -err-duplicate 3

# Manage process and logs
tail -f logs/*.log    # follow latest log
kill $(cat load_test.pid)  # stop the background process
```

### Parameters

| Parameter        | Default        | Description                                                         |
| ---------------- | -------------- | ------------------------------------------------------------------- |
| `-server`        | 127.0.0.1:9001 | gRPC server address                                                 |
| `-accounts`      | 200            | Number of accounts                                                  |
| `-rate`          | 200            | Transactions per second                                             |
| `-fund`          | 10000000000    | Tokens to fund each account                                         |
| `-amount`        | 100            | Tokens per transaction                                              |
| `-minutes`       | 0              | Run for N minutes (0 = unlimited)                                   |
| `-duration`      | 0              | Run duration in seconds (0 = unlimited)                             |
| `-total_txs`     | 0              | Stop after sending N transactions (0 = unlimited)                   |
| `-total_txs`     | 0              | Stop after N tx (0 = unlimited, overrides time)                     |
| `-use-key`       | false          | Use private key instead of zk proof to send transactions (optional) |
| `-err-balance`   | 0              | Inject invalid balance transactions per second (optional)           |
| `-err-nonce`     | 0              | Inject invalid nonce transactions per second (optional)             |
| `-err-duplicate` | 0              | Inject duplicate transactions per second (optional)                 |
| `-err-request`   | 0              | Inject invalid request transactions per second (optional)           |

Notes:

- The script maps positional args to the corresponding flags when invoking `go run .`.
- On Windows, run under Git Bash (this repo includes Bash scripts).
