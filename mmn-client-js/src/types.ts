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

// ----------------- Types Indexer -----------------

export interface Transaction {
  chain_id: string;
  hash: string;
  nonce: number;
  block_hash: string;
  block_number: number;
  block_timestamp: number;
  transaction_index: number;
  from_address: string;
  to_address: string;
  value: string; // uint256 -> string
  gas: number;
  gas_price: string;
  data: string;
  function_selector: string;
  max_fee_per_gas: string;
  max_priority_fee_per_gas: string;
  max_fee_per_blob_gas?: string;
  blob_versioned_hashes?: string[];
  transaction_type: number;
  r: string;
  s: string;
  v: string;
  access_list_json?: string;
  authorization_list_json?: string;
  contract_address?: string;
  gas_used?: number;
  cumulative_gas_used?: number;
  effective_gas_price?: string;
  blob_gas_used?: number;
  blob_gas_price?: string;
  logs_bloom?: string;
  status?: number;
  transaction_timestamp: number;
  text_data: string;
}

export interface Meta {
  chain_id: number;
  address?: string;
  signature?: string;
  page: number;
  limit?: number;
  total_items?: number;
  total_pages?: number;
}

export interface WalletDetail {
  address: string;
  balance: string;
  account_nonce: number;
  last_balance_update: number;
}

export interface WalletDetailResponse {
  data: WalletDetail;
}

export interface ListTransactionResponse {
  meta: Meta;
  data?: Transaction[];
}

export interface TransactionDetailResponse {
  data: {
    transaction: Transaction;
  };
}

// ----------------- Client -----------------

export interface IndexerClientConfig {
  endpoint: string;
  chainId: string;
  timeout?: number;
  headers?: Record<string, string>;
}
