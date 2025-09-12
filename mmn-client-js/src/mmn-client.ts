// MMN Client
// This client provides a complete interface for interacting with MMN blockchain

import axios, { AxiosInstance, AxiosResponse } from 'axios';
import bs58 from 'bs58';
import nacl from 'tweetnacl';
import {
	AddTxResponse,
	ExtraInfo,
	GetAccountByAddressResponse,
	GetCurrentNonceResponse,
	JsonRpcRequest,
	JsonRpcResponse,
	MmnClientConfig,
	SignedTx,
	TxMsg
} from './types';

// --- MMN Client ---

export class MmnClient {
	private config: MmnClientConfig;
	private axiosInstance: AxiosInstance;
	private requestId = 0;

	constructor(config: MmnClientConfig) {
		this.config = {
			timeout: 30000,
			headers: {
				'Content-Type': 'application/json'
			},
			...config
		};

		// Create axios instance
		this.axiosInstance = axios.create({
			baseURL: this.config.baseUrl,
			timeout: this.config.timeout || 30000,
			headers: this.config.headers || {},
			...(this.config.axiosConfig || {})
		});
	}

	private async makeRequest<T>(method: string, params?: unknown): Promise<T> {
		const request: JsonRpcRequest = {
			jsonrpc: '2.0',
			method,
			params,
			id: ++this.requestId
		};

		try {
			const response: AxiosResponse<JsonRpcResponse<T>> = await this.axiosInstance.post('', request);

			const result = response.data;

			if (result.error) {
				throw new Error(`JSON-RPC Error ${result.error.code}: ${result.error.message}`);
			}

			return result.result as T;
		} catch (error) {
			if (axios.isAxiosError(error)) {
				if (error.response) {
					// Server responded with error status
					throw new Error(`HTTP ${error.response.status}: ${error.response.statusText}`);
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

	// --- Transaction Methods ---

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
		privateKey: string;
	}): SignedTx {
		const txMsg: TxMsg = {
			type: params.type,
			sender: params.sender,
			recipient: params.recipient,
			amount: params.amount,
			timestamp: params.timestamp || Date.now(),
			text_data: params.textData || '',
			nonce: params.nonce,
			extra_info: JSON.stringify(params.extraInfo) || ''
		};

		const signature = this.signTransaction(txMsg, params.privateKey);

		return {
			tx_msg: txMsg,
			signature
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
		return bs58.encode(Buffer.from(signature));
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
		privateKey: string;
	}): Promise<AddTxResponse> {
		const signedTx = this.createAndSignTx({ ...params, type: 1 }); // transfer type is always 1 for now
		return this.addTx(signedTx);
	}

	/**
	 * Get current nonce for an account
	 */
	async getCurrentNonce(address: string, tag: 'latest' | 'pending' = 'latest'): Promise<GetCurrentNonceResponse> {
		return this.makeRequest<GetCurrentNonceResponse>('account.getcurrentnonce', { address, tag });
	}

	async getAccountByAddress(address: string): Promise<GetAccountByAddressResponse> {
		return this.makeRequest<GetAccountByAddressResponse>('account.getaccount', { address });
	}

	scaleAmountToDecimals(originalAmount: string | number, decimals: number): string {
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
