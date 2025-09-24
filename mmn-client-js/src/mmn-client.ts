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

// --- MMN Client ---

const TX_TYPE = {
  TRANSFER: 0,
  FAUCET: 1,
};

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

    // Create axios instance
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

  private rawEd25519ToPkcs8Hex(raw: Buffer): string {
    // Helpers: DER building
    const concat = (...parts: Uint8Array[]): Uint8Array => {
      const total = parts.reduce((s, p) => s + p.length, 0);
      const out = new Uint8Array(total);
      let offset = 0;
      for (const p of parts) {
        out.set(p, offset);
        offset += p.length;
      }
      return out;
    };

    const derLen = (n: number): Uint8Array => {
      if (n < 0x80) return Uint8Array.of(n);
      // support up to 4 bytes length which is plenty here
      const bytes: number[] = [];
      let x = n;
      while (x > 0) {
        bytes.unshift(x & 0xff);
        x >>= 8;
      }
      return Uint8Array.of(0x80 | bytes.length, ...bytes);
    };

    const derIntegerZero = Uint8Array.of(0x02, 0x01, 0x00); // INTEGER 0

    // AlgorithmIdentifier = SEQUENCE { OID 1.3.101.112 (ed25519), parameters ABSENT }
    const oidEd25519 = Uint8Array.of(0x06, 0x03, 0x2b, 0x65, 0x70);
    const algId = concat(
      Uint8Array.of(0x30),
      derLen(oidEd25519.length),
      oidEd25519
    );

    // privateKey = OCTET STRING of inner OCTET STRING (RFC8410 commonly seen form)
    const innerOctet = concat(Uint8Array.of(0x04, 0x20), new Uint8Array(raw)); // 0x04, len=0x20, 32 bytes
    const privateKeyField = concat(
      Uint8Array.of(0x04),
      derLen(innerOctet.length),
      innerOctet
    );

    // PrivateKeyInfo = SEQUENCE { version, algId, privateKey }
    const body = concat(derIntegerZero, algId, privateKeyField);
    const pkcs8 = concat(Uint8Array.of(0x30), derLen(body.length), body);

    return Buffer.from(pkcs8).toString('hex');
  }

  // ZkLogin
  public generateEphemeralKeyPair(): IEphemeralKeyPair {
    try {
      const mnemonic = bip39.generateMnemonic(128);

      if (!bip39.validateMnemonic(mnemonic)) {
        throw new Error('Generated mnemonic failed validation');
      }

      const seed = bip39.mnemonicToSeedSync(mnemonic);
      const privateKey = seed.slice(0, 32);

      const kp = nacl.sign.keyPair.fromSeed(privateKey);
      const publicKeyBytes = kp.publicKey;

      const privateKeyHex = this.rawEd25519ToPkcs8Hex(privateKey);
      // const publicKeyHex = this.rawEd25519ToPkcs8Hex(Buffer.from(publicKeyBytes));

      return {
        privateKey: privateKeyHex,
        publicKey: bs58.encode(publicKeyBytes),
      };
    } catch (error) {
      console.error('Error generating wallet:', error);
      throw new Error('Failed to generate wallet');
    }
  }

  // --- Transaction Methods ---

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
   * Sign a transaction with Ed25519
   */
  private signTransaction(tx: TxMsg, privateKeyHex: string): string {
    const serializedData = this.serializeTransaction(tx);

    // Extract the Ed25519 seed from the private key for nacl signing
    const privateKeyDer = Buffer.from(privateKeyHex, 'hex');
    const seed = privateKeyDer.slice(-32);
    const keyPair = nacl.sign.keyPair.fromSeed(seed);

    // Sign using Ed25519 (nacl)
    const signature = nacl.sign.detached(serializedData, keyPair.secretKey);

    if (tx.type === TX_TYPE.FAUCET) {
      return bs58.encode(Buffer.from(signature));
    }

    const userSig = {
      PubKey: Buffer.from(keyPair.publicKey).toString('base64'),
      Sig: Buffer.from(signature).toString('base64'),
    };

    return bs58.encode(Buffer.from(JSON.stringify(userSig)));
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
    address: string,
    tag: 'latest' | 'pending' = 'latest'
  ): Promise<GetCurrentNonceResponse> {
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
    decimals: number
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
