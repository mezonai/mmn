# mmn-client-js

A comprehensive TypeScript client for interacting with the MMN blockchain ecosystem. It provides complete functionality for blockchain transactions, account management, indexer queries, and ZK proof generation.

## Features

- 🔐 **Transaction signing** (Ed25519 with NaCl)
- 📝 **Complete transaction lifecycle** (create, sign, submit)
- 🚀 **JSON-RPC client** for MMN blockchain
- 📊 **Indexer client** for transaction history and wallet details
- � **ZK proof generation** for privacy-preserving authentication
- 💰 **Wallet management** with BIP39 mnemonic support
- 🎯 **User ID to address conversion** with SHA256 hashing
- �📦 **Full TypeScript support** with exported types
- 🌐 **Cross-platform** (Browser & Node.js compatible)

## Installation

```bash
npm install mmn-client-js
```

Or using yarn:

```bash
yarn add mmn-client-js
```

## Quick Start

### Basic MMN Client Usage

```typescript
import { MmnClient, IndexerClient, ZkClient } from 'mmn-client-js';

// Create MMN client for blockchain operations
const mmnClient = new MmnClient({
  baseUrl: 'http://localhost:8080',
  timeout: 30000,
});

// Generate ephemeral key pair for ZK authentication
const keyPair = mmnClient.generateEphemeralKeyPair();
console.log('Public Key:', keyPair.publicKey);

// Convert user ID to blockchain address
const senderAddress = mmnClient.getAddressFromUserId('user123');
const recipientAddress = mmnClient.getAddressFromUserId('user456');

// Get current nonce for sender
const nonceResponse = await mmnClient.getCurrentNonce(senderAddress);
const currentNonce = nonceResponse.nonce;

// Get account information by user ID
const account = await mmnClient.getAccountByUserId('user123');
console.log('Balance:', account.balance);

// Send a transaction
const response = await mmnClient.sendTransaction({
  sender: 'user123',
  recipient: 'user456',
  amount: '1000000000000000000',
  nonce: currentNonce + 1,
  textData: 'Hello MMN!',
  publicKey: keyPair.publicKey,
  privateKey: keyPair.privateKey,
  zkProof: 'zk-proof-string',
  zkPub: 'zk-public-string',
});

if (response.ok) {
  console.log('Transaction Hash:', response.tx_hash);
} else {
  console.error('Transaction failed:', response.error);
}
```

### Indexer Client Usage

```typescript
// Create indexer client for transaction history
const indexerClient = new IndexerClient({
  endpoint: 'https://indexer.mmn.network',
  chainId: 'mmn-mainnet',
});

// Get transaction history for a wallet
const transactions = await indexerClient.getTransactionByWallet(
  'wallet-address',
  1, // page
  50, // limit
  0 // filter: 0=all, 1=received, 2=sent
);

// Get specific transaction details
const tx = await indexerClient.getTransactionByHash('tx-hash');

// Get wallet details
const walletInfo = await indexerClient.getWalletDetail('wallet-address');
```

### ZK Proof Client Usage

```typescript
// Create ZK client for proof generation
const zkClient = new ZkClient({
  endpoint: 'https://zk.mmn.network',
  chainId: 'mmn-mainnet',
});

// Generate ZK proof for authentication
const zkProof = await zkClient.getZkProofs({
  userId: 'user123',
  ephemeralPublicKey: keyPair.publicKey,
  jwt: 'jwt-token',
  address: 'wallet-address',
});
```

## API Reference

### MMN Client Configuration

```typescript
interface MmnClientConfig {
  baseUrl: string; // MMN JSON-RPC base URL
  timeout?: number; // Request timeout in ms (default: 30000)
  headers?: Record<string, string>; // Additional headers
  axiosConfig?: AxiosRequestConfig; // Optional axios overrides
}
```

### Indexer Client Configuration

```typescript
interface IndexerClientConfig {
  endpoint: string; // Indexer API endpoint
  chainId: string; // Blockchain chain ID
  timeout?: number; // Request timeout in ms (default: 30000)
  headers?: Record<string, string>; // Additional headers
}
```

### Core Methods

#### MMN Client Methods

**`generateEphemeralKeyPair(): IEphemeralKeyPair`**
Generate a new Ed25519 key pair using BIP39 mnemonic.

```typescript
const keyPair = client.generateEphemeralKeyPair();
// Returns: { privateKey: string, publicKey: string }
```

**`getAddressFromUserId(userId: string): string`**
Convert user ID to blockchain address using SHA256 + Base58.

```typescript
const address = client.getAddressFromUserId('user123');
```

**`sendTransaction(params): Promise<AddTxResponse>`**
Create, sign, and submit a transaction.

```typescript
const response = await client.sendTransaction({
  sender: 'user123',
  recipient: 'user456',
  amount: '1000000000000000000',
  nonce: 1,
  textData: 'Optional message',
  extraInfo: { type: 'transfer_token', UserSenderId: 'user123', ... },
  publicKey: 'public-key-base58',
  privateKey: 'private-key-pkcs8-hex',
  zkProof: 'zk-proof-string',
  zkPub: 'zk-public-string'
});
```

**`getCurrentNonce(address: string, tag?: 'latest' | 'pending'): Promise<GetCurrentNonceResponse>`**
Get current nonce for an account.

**`getAccountByUserId(userId: string): Promise<GetAccountByAddressResponse>`**
Get account details by user ID.

**`scaleAmountToDecimals(amount: string | number, decimals: number): string`**
Scale amount to blockchain decimals.

#### Indexer Client Methods

**`getTransactionByHash(hash: string): Promise<Transaction>`**
Get transaction details by hash.

**`getTransactionByWallet(wallet: string, page: number, limit: number, filter: number): Promise<ListTransactionResponse>`**
Get transaction history for a wallet.

- `filter`: 0=all, 1=received, 2=sent

**`getWalletDetail(wallet: string): Promise<WalletDetail>`**
Get wallet balance and account information.

#### ZK Client Methods

**`getZkProofs(params): Promise<IZkProof>`**
Generate ZK proof for authentication.

```typescript
const zkProof = await zkClient.getZkProofs({
  userId: 'user123',
  ephemeralPublicKey: 'ephemeral-public-key',
  jwt: 'jwt-token',
  address: 'wallet-address',
});
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For support, please open an issue on the [GitHub repository](https://github.com/mezonai/mmn/issues).
