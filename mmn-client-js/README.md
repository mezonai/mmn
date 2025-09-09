# @mezon/mmn-client

A TypeScript client for interacting with the MMN blockchain via JSON-RPC. It supports creating, signing, and submitting transactions, plus basic account and transaction queries.

## Features

- 🔐 Transaction signing (Ed25519)
- 📝 Create and sign transactions
- 🚀 JSON-RPC client for MMN
- 📦 TypeScript with exported types
- 🌐 Browser & Node.js compatible

## Installation

```bash
npm install @mezonai/mmn-client-js
```

## Quick Start

```typescript
import { MmnClient } from '@mezonai/mmn-client-js';

// Create a client instance
const client = new MmnClient({
  baseUrl: 'http://localhost:8080',
  timeout: 30000
});

// Send a transaction
const response = await client.sendTransaction({
  type: 1, // Transfer type
  sender: 'sender-address',
  recipient: 'recipient-address',
  amount: '1000000000000000000',
  textData: 'Hello MMN!',
  privateKey: 'private-key-hex'
});

if (response.ok) {
  console.log('Transaction Hash:', response.tx_hash);
} else {
  console.error('Error:', response.error);
}
```

## API Reference

### Client Configuration

```typescript
interface MmnClientConfig {
  baseUrl: string;                    // MMN JSON-RPC base URL
  timeout?: number;                   // Request timeout in ms (default: 30000)
  headers?: Record<string, string>;   // Additional headers
  axiosConfig?: AxiosRequestConfig;   // Optional axios overrides
}
```

### Transaction Signing

The client signs transactions with Ed25519. Provide a 64‑byte secret key or a 32‑byte seed (as `Uint8Array` or string hex/base58).

### Transaction Operations

#### `sendTransaction(params): Promise<AddTxResponse>`
Create, sign, and submit a transaction in one call.

```typescript
const response = await client.sendTransaction({
  type: 1,
  sender: 'sender-address',
  recipient: 'recipient-address',
  amount: '1000000000000000000',
  nonce,
  textData: 'Optional message',
  extraInfo: 'Optional extra',
  privateKey: new Uint8Array(/* 64-byte secretKey */)
});
```

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Support

For support, please open an issue on the [GitHub repository](https://github.com/mezonai/mmn/issues).
