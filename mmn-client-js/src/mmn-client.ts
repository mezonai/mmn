// MMN Client
// This client provides a complete interface for interacting with MMN blockchain
import axios, { AxiosInstance, AxiosResponse } from 'axios';
import * as bip39 from 'bip39';
import bs58 from 'bs58';
import { createHash } from 'crypto';
import nacl from 'tweetnacl';
import {
  AddTxResponse,
  ExtraInfo,
  GetAccountByAddressResponse,
  GetCurrentNonceResponse,
  IEphemeralKeyPair,
  JsonRpcRequest,
  JsonRpcResponse,
  MmnClientConfig,
  SignedTx,
  TxMsg,
} from './types';

// Cryptographic constants
const CRYPTO_CONSTANTS = {
  ED25519_PRIVATE_KEY_LENGTH: 32,
  ED25519_PUBLIC_KEY_LENGTH: 32,
  MNEMONIC_ENTROPY_BITS: 128,
  PKCS8_VERSION: 0,
  ASN1_SEQUENCE_TAG: 0x30,
  ASN1_OCTET_STRING_TAG: 0x04,
  ASN1_INTEGER_TAG: 0x02,
} as const;

const TX_TYPE = {
  TRANSFER: 0,
  FAUCET: 1,
} as const;

const DECIMALS = 6;

export class MmnClient {
  private config: MmnClientConfig;
  private axiosInstance: AxiosInstance;
  private requestId = 0;

  constructor(config: MmnClientConfig) {
    this.config = {
      timeout: 30000,
      headers: {
        'Content-Type': 'application/json',
      },
      ...config,
    };

    this.axiosInstance = axios.create({
      baseURL: this.config.baseUrl,
      timeout: this.config.timeout || 30000,
      headers: this.config.headers || {},
      ...(this.config.axiosConfig || {}),
    });
  }

  private async makeRequest<T>(method: string, params?: unknown): Promise<T> {
    const request: JsonRpcRequest = {
      jsonrpc: '2.0',
      method,
      params,
      id: ++this.requestId,
    };

    try {
      const response: AxiosResponse<JsonRpcResponse<T>> =
        await this.axiosInstance.post('', request);

      const result = response.data;

      if (result.error) {
        throw new Error(
          `JSON-RPC Error ${result.error.code}: ${result.error.message}`
        );
      }

      return result.result as T;
    } catch (error) {
      if (axios.isAxiosError(error)) {
        if (error.response) {
          // Server responded with error status
          throw new Error(
            `HTTP ${error.response.status}: ${error.response.statusText}`
          );
        } else if (error.request) {
          // Request was made but no response received
          throw new Error('Network error: No response received');
        } else {
          // Something else happened
          throw new Error(`Request error: ${error.message}`);
        }
      }

      if (error instanceof Error) {
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
  private rawEd25519ToPkcs8Hex(raw: Buffer): string {
    // Input validation
    if (!Buffer.isBuffer(raw)) {
      throw new Error('Private key must be a Buffer');
    }

    if (raw.length !== CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH) {
      throw new Error(
        `Ed25519 private key must be exactly ${CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH} bytes`
      );
    }

    try {
      // Ed25519 OID: 1.3.101.112 (constant-time)
      const ED25519_OID = Buffer.from([0x06, 0x03, 0x2b, 0x65, 0x70]);
      const VERSION_BYTES = Buffer.from([
        CRYPTO_CONSTANTS.ASN1_INTEGER_TAG,
        0x01,
        CRYPTO_CONSTANTS.PKCS8_VERSION,
      ]); // INTEGER 0

      // Create algorithm identifier sequence
      const algorithmId = Buffer.concat([
        Buffer.from([0x30, 0x0b]), // SEQUENCE, length 11
        ED25519_OID,
      ]);

      // Create private key octet string
      const privateKeyOctetString = Buffer.concat([
        Buffer.from([CRYPTO_CONSTANTS.ASN1_OCTET_STRING_TAG, 0x22]), // OCTET STRING, length 34
        Buffer.from([CRYPTO_CONSTANTS.ASN1_OCTET_STRING_TAG, 0x20]), // inner OCTET STRING, length 32
        raw,
      ]);

      // Create PKCS#8 body
      const pkcs8Body = Buffer.concat([
        VERSION_BYTES,
        algorithmId,
        privateKeyOctetString,
      ]);

      // Create final PKCS#8 structure
      const pkcs8 = Buffer.concat([
        Buffer.from([CRYPTO_CONSTANTS.ASN1_SEQUENCE_TAG]), // SEQUENCE
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

  private encodeLength(length: number): Buffer {
    if (length < 0x80) {
      return Buffer.from([length]);
    }
    const bytes = [];
    let len = length;
    while (len > 0) {
      bytes.unshift(len & 0xff);
      len >>= 8;
    }
    return Buffer.from([0x80 | bytes.length, ...bytes]);
  }

  /**
   * Securely generate ephemeral key pair with proper entropy
   * @returns Ephemeral key pair with private and public keys
   * @throws Error if key generation fails
   */
  public generateEphemeralKeyPair(): IEphemeralKeyPair {
    try {
      // Generate cryptographically secure mnemonic
      const mnemonic = bip39.generateMnemonic(
        CRYPTO_CONSTANTS.MNEMONIC_ENTROPY_BITS
      );

      if (!bip39.validateMnemonic(mnemonic)) {
        throw new Error('Generated mnemonic failed validation');
      }

      // Convert mnemonic to seed
      const seed = bip39.mnemonicToSeedSync(mnemonic);

      // Extract first 32 bytes as private key
      const privateKey = seed.subarray(
        0,
        CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH
      );

      // Validate private key length
      if (privateKey.length !== CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH) {
        throw new Error(
          `Invalid private key length: expected ${CRYPTO_CONSTANTS.ED25519_PRIVATE_KEY_LENGTH}, got ${privateKey.length}`
        );
      }

      // Generate key pair from seed
      const keyPair = nacl.sign.keyPair.fromSeed(privateKey);
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
      const privateKeyHex = this.rawEd25519ToPkcs8Hex(privateKey);

      // Clear sensitive data
      seed.fill(0);
      privateKey.fill(0);
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
    const hash = createHash('sha256').update(userId, 'utf8').digest();
    return bs58.encode(hash);
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
      const privateKeyDer = Buffer.from(privateKeyHex, 'hex');

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
        return bs58.encode(Buffer.from(signature));
      }

      // For regular transactions, wrap signature with public key
      const userSig = {
        PubKey: Buffer.from(keyPair.publicKey).toString('base64'),
        Sig: Buffer.from(signature).toString('base64'),
      };

      return bs58.encode(Buffer.from(JSON.stringify(userSig)));
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
  private serializeTransaction(tx: TxMsg): Buffer {
    const data = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}|${tx.extra_info}`;
    return Buffer.from(data, 'utf8');
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
  async sendTransaction(params: {
    sender: string;
    recipient: string;
    amount: string;
    nonce: number;
    timestamp?: number;
    textData?: string;
    extraInfo?: ExtraInfo;
    publicKey: string;
    privateKey: string;
    zkProof: string;
    zkPub: string;
  }): Promise<AddTxResponse> {
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
