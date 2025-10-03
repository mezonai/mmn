## Usage

### 1. Create migration sender wallet

```bash
cd mezon-migration

# Create actual wallet (creates wallet, funds it, saves to file)
go run . -create-sender-wallet -mmn-endpoint=localhost:9001 -database-url=postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable -faucet-private-key=302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee
```

### 2. Run user migration (Requires database and migration wallet file)

```bash
cd mezon-migration

# Run actual migration
go run . -run-migration -mmn-endpoint=localhost:9001 -database-url=postgres://mezon:m3z0n@localhost:5432/mezon?sslmode=disable
```

## Command Line Options

| Option                  | Description                                 | Default Value |
| ------------------------| ------------------------------------------- | ------------- |
| `-create-sender-wallet` | Create migration wallet only                | `false`       |
| `-run-migration`        | Run user migration only                     | `false`       |
| `-mmn-endpoint`         | Endpoint of MMN node                        | `''`          |
| `-database-url`         | Database host                               | `''`          |
| `-faucet-private-key`   | Faucet private key hex                      | `''`          |