// --- JSON-RPC Types ---

import { AxiosRequestConfig } from 'axios';

export interface JsonRpcRequest {
	jsonrpc: '2.0';
	method: string;
	params?: unknown;
	id: string | number;
}

export interface JsonRpcError {
	code: number;
	message: string;
	data?: unknown;
}

export interface JsonRpcResponse<T = unknown> {
	jsonrpc: '2.0';
	result?: T;
	error?: JsonRpcError;
	id: string | number;
}

// --- Transaction Types ---

export enum ETransferType {
	GiveCoffee = 'give_coffee',
	TransferToken = 'transfer_token',
	UnlockItem = 'unlock_item'
}

export interface ExtraInfo {
	type: ETransferType;
	ItemId?: string;
	ItemType?: string;
	ClanId?: string;
	UserSenderId: string;
	UserSenderUsername: string;
	UserReceiverId: string;
	ChannelId?: string;
	MessageRefId?: string;
}

export interface TxMsg {
	type: number;
	sender: string;
	recipient: string;
	amount: string;
	timestamp: number;
	text_data: string;
	nonce: number;
	extra_info: string;
}

export interface SignedTx {
	tx_msg: TxMsg;
	signature: string;
}

export interface AddTxResponse {
	ok: boolean;
	tx_hash: string;
	error: string;
}

export interface GetCurrentNonceResponse {
	address: string;
	nonce: number;
	tag: string;
	error: string;
}

export interface GetAccountByAddressResponse {
	address: string;
	balance: string;
	nonce: number;	
	decimals: number;
}

// --- Client Configuration ---

export interface MmnClientConfig {
	baseUrl: string;
	timeout?: number;
	headers?: Record<string, string>;
	axiosConfig?: AxiosRequestConfig;
}
