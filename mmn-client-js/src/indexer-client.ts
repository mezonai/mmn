import axios, { AxiosInstance } from 'axios';
import {
  IndexerClientConfig,
  ListTransactionResponse,
  Transaction,
  TransactionDetailResponse,
  WalletDetail,
  WalletDetailResponse,
} from './types';

const API_FILTER_PARAMS = {
  ALL: 0,
  SENT: 2,
  RECEIVED: 1,
};

export class IndexerClient {
  private axiosInstance: AxiosInstance;
  private endpoint: string;
  private chainId: string;

  constructor(config: IndexerClientConfig) {
    this.endpoint = config.endpoint;
    this.chainId = config.chainId;

    this.axiosInstance = axios.create({
      baseURL: this.endpoint,
      timeout: config.timeout || 30000,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...(config.headers || {}),
      },
    });
  }

  private async makeRequest<T>(
    method: 'GET' | 'POST',
    path: string,
    params?: Record<string, string | number>,
    body?: any
  ): Promise<T> {
    const resp = await this.axiosInstance.request<T>({
      method,
      url: path,
      params,
      data: body,
    });
    return resp.data;
  }

  async getTransactionByHash(hash: string): Promise<Transaction> {
    const path = `${this.chainId}/tx/${hash}/detail`;
    const res = await this.makeRequest<TransactionDetailResponse>('GET', path);
    return res.data.transaction;
  }

  async getTransactionByWallet(
    wallet: string,
    page = 1,
    limit = 50,
    filter: number,
    sortBy = 'transaction_timestamp',
    sortOrder: 'asc' | 'desc' = 'desc'
  ): Promise<ListTransactionResponse> {
    if (!wallet) {
      throw new Error('wallet address cannot be empty');
    }

    if (page < 1) page = 1;
    if (limit <= 0) limit = 50;
    if (limit > 1000) limit = 1000;

    const params: Record<string, string | number> = {
      page: page - 1,
      limit,
      sort_by: sortBy,
      sort_order: sortOrder,
    };

    switch (filter) {
      case API_FILTER_PARAMS.ALL:
        params['wallet_address'] = wallet;
        break;
      case API_FILTER_PARAMS.SENT:
        params['filter_from_address'] = wallet;
        break;
      case API_FILTER_PARAMS.RECEIVED:
        params['filter_to_address'] = wallet;
        break;
      default:
        break;
    }

    const path = `${this.chainId}/transactions`;
    return this.makeRequest<ListTransactionResponse>('GET', path, params);
  }

  async getWalletDetail(wallet: string): Promise<WalletDetail> {
    if (!wallet) {
      throw new Error('wallet address cannot be empty');
    }

    const path = `${this.chainId}/wallets/${wallet}/detail`;
    const res = await this.makeRequest<WalletDetailResponse>('GET', path);
    return res.data;
  }
}
