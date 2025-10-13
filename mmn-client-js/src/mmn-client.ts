// MMN Client
// This client provides a complete interface for interacting with MMN blockchain
import bs58 from 'bs58';
import nacl from 'tweetnacl';
import CryptoJS from 'crypto-js';
import {
  AddTxResponse,
  ExtraInfo,
  GetAccountByAddressResponse,
  GetCurrentNonceResponse,
  IEphemeralKeyPair,
  JsonRpcRequest,
  JsonRpcResponse,
  MmnClientConfig,
  SendTransactionRequest,
  SignedTx,
  TxMsg,
} from './types';

// Buffer polyfill for mobile environments
class BufferPolyfill {
  static isBuffer(obj: any): boolean {
    if (typeof Buffer !== 'undefined' && Buffer.isBuffer) {
      return Buffer.isBuffer(obj);
    }
    return obj instanceof Uint8Array;
  }

  static from(data: any, encoding?: string): any {
    if (typeof Buffer !== 'undefined' && Buffer.from) {
      return Buffer.from(data, encoding as any);
    }

    let result: Uint8Array;

    if (Array.isArray(data)) {
      result = new Uint8Array(data);
    } else if (typeof data === 'string') {
      if (encoding === 'hex') {
        const bytes = [];
        for (let i = 0; i < data.length; i += 2) {
          bytes.push(parseInt(data.substr(i, 2), 16));
        }
        result = new Uint8Array(bytes);
      } else {
        // UTF-8 encoding
        const encoder = new TextEncoder();
        result = encoder.encode(data);
      }
    } else if (data instanceof Uint8Array) {
      result = data;
    } else {
      result = new Uint8Array(data);
    }

    // Add toString method
    (result as any).toString = function (encoding?: string) {
      if (encoding === 'hex') {
        return Array.from(this as Uint8Array)
          .map((b: number) => b.toString(16).padStart(2, '0'))
          .join('');
      } else if (encoding === 'base64') {
        // Simple base64 encoding
        const bytes = this as Uint8Array;
        let binary = '';
        for (let i = 0; i < bytes.length; i++) {
          binary += String.fromCharCode(bytes[i]);
        }
        return btoa(binary);
      } else {
        // UTF-8 decoding
        const decoder = new TextDecoder();
        return decoder.decode(this as Uint8Array);
      }
    };

    return result;
  }

  static concat(arrays: any[]): any {
    if (typeof Buffer !== 'undefined' && Buffer.concat) {
      return Buffer.concat(arrays);
    }

    const totalLength = arrays.reduce((sum, arr) => sum + arr.length, 0);
    const result = new Uint8Array(totalLength);
    let offset = 0;

    for (const arr of arrays) {
      result.set(arr, offset);
      offset += arr.length;
    }

    // Add toString method
    (result as any).toString = function (encoding?: string) {
      if (encoding === 'hex') {
        return Array.from(this as Uint8Array)
          .map((b: number) => b.toString(16).padStart(2, '0'))
          .join('');
      } else {
        const decoder = new TextDecoder();
        return decoder.decode(this as Uint8Array);
      }
    };

    return result;
  }
}

// Use polyfill if Buffer is not available
const BufferCompat = typeof Buffer !== 'undefined' ? Buffer : BufferPolyfill;

// Cryptographic constants
const CRYPTO_CONSTANTS = {
  ED25519_PRIVATE_KEY_LENGTH: 32,
  ED25519_PUBLIC_KEY_LENGTH: 32,
  MNEMONIC_ENTROPY_BITS: 128,
  PKCS8_VERSION: 0,

  // ASN.1 DER encoding
  ASN1_SEQUENCE_TAG: 0x30,
  ASN1_OCTET_STRING_TAG: 0x04,
  ASN1_INTEGER_TAG: 0x02,
  ASN1_LENGTH: 0x80,

  // Ed25519 OID bytes: 1.3.101.112 (RFC 8410)
  ED25519_OID_BYTES: [0x06, 0x03, 0x2b, 0x65, 0x70],

  // PKCS#8 structure length constants
  PKCS8_ALGORITHM_ID_LENGTH: 0x0b, // SEQUENCE length for algorithm identifier
  PKCS8_PRIVATE_KEY_OCTET_OUTER_LENGTH: 0x22, // Outer OCTET STRING length (34 bytes)
  PKCS8_PRIVATE_KEY_OCTET_INNER_LENGTH: 0x20, // Inner OCTET STRING length (32 bytes)
} as const;

// PRNG (Pseudo-Random Number Generator) constants
const PRNG_CONSTANTS = {
  // Numerical Recipes LCG
  LCG_MULTIPLIER: 1664525, // LCG multiplier (from Numerical Recipes)
  LCG_INCREMENT: 1013904223, // LCG increment
  LCG_MODULUS: 4294967296, // 2^32 modulus for LCG

  TIMESTAMP_MULTIPLIER: 2654435761, // Golden Ratio constant
  HASH_SUBSTRING_LENGTH: 8,
  BYTE_SHIFT: 24,
  BYTE_MASK: 0xff,
} as const;

const TX_TYPE = {
  TRANSFER: 0,
  FAUCET: 1,
} as const;

const DECIMALS = 6;

export class MmnClient {
  private config: MmnClientConfig;
  private requestId = 0;

  constructor(config: MmnClientConfig) {
    this.config = {
      timeout: 30000,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
      },
      ...config,
    };
  }

  private async makeRequest<T>(method: string, params?: unknown): Promise<T> {
    const request: JsonRpcRequest = {
      jsonrpc: '2.0',
      method,
      params,
      id: ++this.requestId,
    };

    // Create AbortController for timeout
    const controller = new AbortController();
    const timeoutId = setTimeout(
      () => controller.abort(),
      this.config.timeout || 30000
    );

    try {
      const requestOptions: RequestInit = {
        method: 'POST',
        mode: 'cors',
        credentials: 'omit',
        signal: controller.signal,
        headers: this.config.headers || {},
        body: JSON.stringify(request),
      };

      const response = await fetch(this.config.baseUrl, requestOptions);
      clearTimeout(timeoutId);

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }

      const result: JsonRpcResponse<T> = await response.json();

      if (result.error) {
        throw new Error(
          `JSON-RPC Error ${result.error.code}: ${result.error.message}`
        );
      }

      return result.result as T;
    } catch (error) {
      clearTimeout(timeoutId);

      if (error instanceof Error) {
        if (error.name === 'AbortError') {
          throw new Error(
            `Request timeout after ${this.config.timeout || 30000}ms`
          );
        }
        throw error;
      }
      throw new Error('Unknown error occurred');
    }
  }

  /**
   * Securely convert raw Ed25519 private key to PKCS#8 format
   * @param raw - Raw 32-byte Ed25519 private key
   * @returns PKCS#8 formatted private key in hex
   * @throws Error if input validation fails
   */
  private rawEd25519ToPkcs8Hex(raw: Uint8Array | Buffer): string {
    // Input validation
    if (!BufferCompat.isBuffer(raw)) {
      throw new Error('Private key must be a Buffer');
    }

    if (raw.length !== CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH) {
      throw new Error(
        `Ed25519 private key must be exactly ${CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH} bytes`
      );
    }

    try {
      // Ed25519 OID: 1.3.101.112 (RFC 8410 - Algorithm Identifiers for Ed25519)
      const ED25519_OID = BufferCompat.from(CRYPTO_CONSTANTS.ED25519_OID_BYTES);

      const VERSION_BYTES = BufferCompat.from([
        CRYPTO_CONSTANTS.ASN1_INTEGER_TAG,
        0x01, // Length of integer (1 byte)
        CRYPTO_CONSTANTS.PKCS8_VERSION,
      ]);

      // Create algorithm identifier sequence (AlgorithmIdentifier)
      const algorithmId = BufferCompat.concat([
        BufferCompat.from([
          CRYPTO_CONSTANTS.ASN1_SEQUENCE_TAG,
          CRYPTO_CONSTANTS.PKCS8_ALGORITHM_ID_LENGTH,
        ]),
        ED25519_OID,
      ]);

      // Create private key octet string (wrapped Ed25519 private key)
      const privateKeyOctetString = BufferCompat.concat([
        BufferCompat.from([
          CRYPTO_CONSTANTS.ASN1_OCTET_STRING_TAG,
          CRYPTO_CONSTANTS.PKCS8_PRIVATE_KEY_OCTET_OUTER_LENGTH,
        ]), // OCTET STRING, length 34
        BufferCompat.from([
          CRYPTO_CONSTANTS.ASN1_OCTET_STRING_TAG,
          CRYPTO_CONSTANTS.PKCS8_PRIVATE_KEY_OCTET_INNER_LENGTH,
        ]), // inner OCTET STRING, length 32
        raw,
      ]);

      // Create PKCS#8 body
      const pkcs8Body = BufferCompat.concat([
        VERSION_BYTES,
        algorithmId,
        privateKeyOctetString,
      ]);

      // Create final PKCS#8 structure
      const pkcs8 = BufferCompat.concat([
        BufferCompat.from([CRYPTO_CONSTANTS.ASN1_SEQUENCE_TAG]), // SEQUENCE
        this.encodeLength(pkcs8Body.length),
        pkcs8Body,
      ]);

      const result = pkcs8.toString('hex');

      // Clear sensitive data from memory
      raw.fill(0);
      privateKeyOctetString.fill(0);
      pkcs8Body.fill(0);
      pkcs8.fill(0);

      return result;
    } catch (error) {
      // Clear sensitive data on error
      raw.fill(0);
      throw new Error(
        `Failed to convert private key to PKCS#8: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`
      );
    }
  }

  /**
   * Encode length in ASN.1 DER format
   * ASN.1 length encoding rules:
   * - Short form (0-127): single byte with the length value
   * - Long form (128+): first byte is 0x80 + number of length bytes, followed by length bytes
   * @param length - The length value to encode
   * @returns ASN.1 DER encoded length bytes
   */
  private encodeLength(length: number): Uint8Array {
    if (length < CRYPTO_CONSTANTS.ASN1_LENGTH) {
      return BufferCompat.from([length]);
    }
    const bytes = [];
    let len = length;
    while (len > 0) {
      bytes.unshift(len & PRNG_CONSTANTS.BYTE_MASK);
      len >>= 8;
    }

    return BufferCompat.from([
      CRYPTO_CONSTANTS.ASN1_LENGTH | bytes.length,
      ...bytes,
    ]);
  }

  /**
   * Generate secure entropy using multiple sources for maximum compatibility
   * @returns Array of 32 random bytes
   */
  private generateSecureEntropy(): number[] {
    const entropy: number[] = [];
    const targetLength = CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH;

    // Use multiple entropy sources
    const now = Date.now();
    const performance =
      typeof window !== 'undefined' && window.performance
        ? window.performance.now()
        : now;
    const random = Math.random();

    // Create initial seed from timestamp and random
    let seed = now + performance + random;

    // Generate bytes using Linear Congruential Generator (LCG) with multiple entropy sources
    // This provides a fallback Pseudorandom Number Generator (PRNG) when crypto.getRandomValues is not available
    for (let i = 0; i < targetLength; i++) {
      // Xₙ₊₁ = (a * Xₙ + c) mod m
      seed =
        (seed * PRNG_CONSTANTS.LCG_MULTIPLIER + PRNG_CONSTANTS.LCG_INCREMENT) %
        PRNG_CONSTANTS.LCG_MODULUS;
      // Mix with timestamp to add time-based entropy
      seed ^= (now + i) * PRNG_CONSTANTS.TIMESTAMP_MULTIPLIER;
      // Mix with Math.random() for additional browser-provided randomness
      seed ^= Math.floor(Math.random() * PRNG_CONSTANTS.LCG_MODULUS);

      // Additional cryptographic mixing using SHA256 if CryptoJS is available
      if (typeof CryptoJS !== 'undefined') {
        const hashInput = seed.toString() + i.toString() + now.toString();
        const hash = CryptoJS.SHA256(hashInput).toString();
        // Extract first 8 hex characters (32 bits) from hash for mixing
        seed ^= parseInt(
          hash.substring(0, PRNG_CONSTANTS.HASH_SUBSTRING_LENGTH),
          16
        );
      }

      entropy.push(
        (seed >>> PRNG_CONSTANTS.BYTE_SHIFT) & PRNG_CONSTANTS.BYTE_MASK
      );
    }

    return entropy;
  }

  /**
   * Securely generate ephemeral key pair with proper entropy
   * React Native compatible version with multiple fallbacks
   * @returns Ephemeral key pair with private and public keys
   * @throws Error if key generation fails
   */
  public generateEphemeralKeyPair(): IEphemeralKeyPair {
    try {
      let seed: Uint8Array;

      // Try multiple approaches for mobile compatibility
      try {
        // Method 1: Try nacl.randomBytes first
        seed = nacl.randomBytes(CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH);
      } catch (naclError) {
        try {
          // Method 2: Use crypto.getRandomValues if available (browsers/React Native)
          if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
            seed = new Uint8Array(CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH);
            crypto.getRandomValues(seed);
          } else {
            throw new Error('crypto.getRandomValues not available');
          }
        } catch (cryptoError) {
          // Method 3: Fallback to secure pseudo-random generation
          const entropy = this.generateSecureEntropy();
          seed = new Uint8Array(entropy);
        }
      }

      // Validate seed length
      if (seed.length !== CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH) {
        throw new Error(
          `Invalid seed length: expected ${CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH}, got ${seed.length}`
        );
      }

      // Generate key pair from seed
      const keyPair = nacl.sign.keyPair.fromSeed(seed);
      const publicKeyBytes = keyPair.publicKey;

      // Validate public key
      if (
        publicKeyBytes.length !== CRYPTO_CONSTANTS.ED25519_PUBLIC_KEY_LENGTH
      ) {
        throw new Error(
          `Invalid public key length: expected ${CRYPTO_CONSTANTS.ED25519_PUBLIC_KEY_LENGTH}, got ${publicKeyBytes.length}`
        );
      }

      // Convert private key to PKCS#8 format
      const privateKeyHex = this.rawEd25519ToPkcs8Hex(BufferCompat.from(seed));

      // Clear sensitive data
      seed.fill(0);
      keyPair.secretKey.fill(0);

      return {
        privateKey: privateKeyHex,
        publicKey: bs58.encode(publicKeyBytes),
      };
    } catch (error) {
      throw new Error(
        `Failed to generate ephemeral key pair: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`
      );
    }
  }

  public getAddressFromUserId(userId: string): string {
    // Use crypto-js for SHA-256 in React Native
    const hash = CryptoJS.SHA256(userId).toString(CryptoJS.enc.Hex);
    const hashBuffer = BufferCompat.from(hash, 'hex');
    return bs58.encode(hashBuffer);
  }

  /**
   * Create and sign a transaction message
   */
  private createAndSignTx(params: {
    type: number;
    sender: string;
    recipient: string;
    amount: string;
    timestamp?: number;
    textData?: string;
    nonce: number;
    extraInfo?: ExtraInfo;
    publicKey: string;
    privateKey: string;
    zkProof: string;
    zkPub: string;
  }): SignedTx {
    const fromAddress = this.getAddressFromUserId(params.sender);
    const toAddress = this.getAddressFromUserId(params.recipient);

    const txMsg: TxMsg = {
      type: params.type,
      sender: fromAddress,
      recipient: toAddress,
      amount: params.amount,
      timestamp: params.timestamp || Date.now(),
      text_data: params.textData || '',
      nonce: params.nonce,
      extra_info: JSON.stringify(params.extraInfo) || '',
      zk_proof: params.zkProof,
      zk_pub: params.zkPub,
    };

    const signature = this.signTransaction(txMsg, params.privateKey);

    return {
      tx_msg: txMsg,
      signature,
    };
  }

  /**
   * Securely sign a transaction with Ed25519
   * @param tx - Transaction message to sign
   * @param privateKeyHex - Private key in PKCS#8 hex format
   * @returns Base58 encoded signature
   * @throws Error if signing fails
   */
  private signTransaction(tx: TxMsg, privateKeyHex: string): string {
    try {
      // Validate inputs
      if (!tx || typeof tx !== 'object') {
        throw new Error('Invalid transaction object');
      }

      if (!privateKeyHex || typeof privateKeyHex !== 'string') {
        throw new Error('Invalid private key format');
      }

      // Serialize transaction data
      const serializedData = this.serializeTransaction(tx);

      if (!serializedData || serializedData.length === 0) {
        throw new Error('Failed to serialize transaction');
      }

      // Extract the Ed25519 seed from the private key for nacl signing
      const privateKeyDer = BufferCompat.from(privateKeyHex, 'hex');

      if (privateKeyDer.length < CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH) {
        throw new Error(
          `Invalid private key length: expected at least ${CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH} bytes, got ${privateKeyDer.length}`
        );
      }

      const seed = privateKeyDer.subarray(
        -CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH
      ); // Last 32 bytes
      const keyPair = nacl.sign.keyPair.fromSeed(seed);

      // Validate key pair
      if (!keyPair.publicKey || !keyPair.secretKey) {
        throw new Error('Failed to create key pair from seed');
      }

      // Sign using Ed25519 (nacl) - constant time operation
      const signature = nacl.sign.detached(serializedData, keyPair.secretKey);

      if (!signature || signature.length === 0) {
        throw new Error('Failed to generate signature');
      }

      // Clear sensitive data
      privateKeyDer.fill(0);
      seed.fill(0);
      keyPair.secretKey.fill(0);

      // Return signature based on transaction type
      if (tx.type === TX_TYPE.FAUCET) {
        return bs58.encode(BufferCompat.from(signature));
      }

      // For regular transactions, wrap signature with public key
      const userSig = {
        PubKey: BufferCompat.from(keyPair.publicKey).toString('base64'),
        Sig: BufferCompat.from(signature).toString('base64'),
      };

      return bs58.encode(BufferCompat.from(JSON.stringify(userSig)));
    } catch (error) {
      throw new Error(
        `Transaction signing failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`
      );
    }
  }

  /**
   * Serialize transaction for signing
   */
  private serializeTransaction(tx: TxMsg): Uint8Array {
    const data = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}|${tx.extra_info}`;
    return BufferCompat.from(data, 'utf8');
  }

  /**
   * Add a signed transaction to the blockchain
   */
  private async addTx(signedTx: SignedTx): Promise<AddTxResponse> {
    return this.makeRequest<AddTxResponse>('tx.addtx', signedTx);
  }

  /**
   * Send a transaction (create, sign, and submit)
   */
  async sendTransaction(
    params: SendTransactionRequest
  ): Promise<AddTxResponse> {
    const signedTx = this.createAndSignTx({
      ...params,
      type: TX_TYPE.TRANSFER,
    });
    return this.addTx(signedTx);
  }

  /**
   * Get current nonce for an account
   */
  async getCurrentNonce(
    userId: string,
    tag: 'latest' | 'pending' = 'latest'
  ): Promise<GetCurrentNonceResponse> {
    const address = this.getAddressFromUserId(userId);
    return this.makeRequest<GetCurrentNonceResponse>(
      'account.getcurrentnonce',
      { address, tag }
    );
  }

  async getAccountByUserId(
    userId: string
  ): Promise<GetAccountByAddressResponse> {
    const address = this.getAddressFromUserId(userId);
    return this.makeRequest<GetAccountByAddressResponse>('account.getaccount', {
      address,
    });
  }

  scaleAmountToDecimals(
    originalAmount: string | number,
    decimals = DECIMALS
  ): string {
    let scaledAmount = BigInt(originalAmount);
    for (let i = 0; i < decimals; i++) {
      scaledAmount = scaledAmount * BigInt(10);
    }
    return scaledAmount.toString();
  }
}

export function createMmnClient(config: MmnClientConfig): MmnClient {
  return new MmnClient(config);
}

export default MmnClient;
